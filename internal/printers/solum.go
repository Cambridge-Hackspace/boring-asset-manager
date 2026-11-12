package printers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// SoluM's electronic shelf labels (docs/printing.md#solum), pushed through
// SoluM's AIMS SaaS "Common API", as its public documentation at
// eu.common.solumesl.com/docs describes it: BAM! signs in for a bearer
// token, checks the label is the model's size and inks, pushes its image,
// and asks a little while whether the label has shown it.

// DefaultSolumBase is AIMS SaaS in Europe; other regions have their own.
const DefaultSolumBase = "https://eu.common.solumesl.com"

// SolumAccount is the AIMS account labels are pushed through: its API, the
// company and store the labels are in, and who signs in.
type SolumAccount struct {
	Base     string
	Company  string
	Store    string
	Username string
	Password string
}

// solumAddress is a solum connection's address: a label's code, and the
// page the image goes to, 1 unless it says.
type solumAddress struct {
	Label string
	Page  int
}

func (a solumAddress) String() string {
	if a.Page == 1 {
		return a.Label
	}
	return a.Label + "?page=" + strconv.Itoa(a.Page)
}

// solumLabel is a SoluM label's code, printed on its back.
var solumLabel = regexp.MustCompile(`^[0-9A-Z]{8,20}$`)

// parseSolumAddress reads a label's code, then optionally ?page= and its
// page, 1 to 10.
func parseSolumAddress(s string) (solumAddress, error) {
	code, query, _ := strings.Cut(strings.TrimSpace(s), "?")
	a := solumAddress{Label: strings.ToUpper(strings.ReplaceAll(code, ":", "")), Page: 1}
	if !solumLabel.MatchString(a.Label) {
		return a, errors.New("a solum connection's address is the label's code, from its back, such as 04507B0AC391, then optionally ?page= and a page from 1 to 10")
	}
	if query != "" {
		v, err := url.ParseQuery(query)
		if err != nil || len(v) != 1 || len(v["page"]) != 1 {
			return a, errors.New("a solum connection's address takes only ?page=, from 1 to 10")
		}
		n, err := strconv.Atoi(v.Get("page"))
		if err != nil || n < 1 || n > 10 {
			return a, errors.New("a SoluM label's page is from 1 to 10")
		}
		a.Page = n
	}
	return a, nil
}

// solumColors are the inks AIMS names a label's colors by.
var solumColors = map[string]labels.Palette{"BW": labels.PaletteBW, "BWR": labels.PaletteBWR, "BWY": labels.PaletteBWY, "BWYR": labels.PaletteBWRY, "BWRY": labels.PaletteBWRY}

// solumTries is how many times a push is tried while AIMS fails, and
// solumWait how long BAM! asks whether the label has shown it, every
// solumPoll: a print waits for it, and has little time.
var (
	solumTries = 3
	solumWait  = 8 * time.Second
	solumPoll  = 2 * time.Second
)

// SolumClient pushes to AIMS, keeping its token between pushes.
type SolumClient struct {
	HTTP *http.Client

	mu       sync.Mutex
	signedIn string // the account the tokens are for
	access   string
	refresh  string
}

// solumAnswer is how AIMS answers: a code, and a message that is a string
// or, for a token, an object.
type solumAnswer struct {
	ResponseCode    string          `json:"responseCode"`
	ResponseMessage json.RawMessage `json:"responseMessage"`
	CustomBatchID   string          `json:"customBatchId"`
}

func (a solumAnswer) message() string {
	var s string
	if json.Unmarshal(a.ResponseMessage, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(a.ResponseMessage))
}

// errSolumAuth is an answer that the token is not, or no longer, good.
var errSolumAuth = errors.New("AIMS refused the token")

// call makes one request to AIMS with the token, answering what it said.
func (c *SolumClient) call(ctx context.Context, method, url, token string, body any) (solumAnswer, []byte, int, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return solumAnswer{}, nil, 0, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return solumAnswer{}, nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return solumAnswer{}, nil, 0, fmt.Errorf("reaching AIMS: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var a solumAnswer
	_ = json.Unmarshal(raw, &a)
	if resp.StatusCode == http.StatusUnauthorized {
		return a, raw, resp.StatusCode, errSolumAuth
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		said := a.message()
		if said == "" {
			said = strings.TrimSpace(string(raw[:min(len(raw), 300)]))
		}
		return a, raw, resp.StatusCode, fmt.Errorf("AIMS answered HTTP %s: %s", resp.Status, said)
	}
	if a.ResponseCode != "" && a.ResponseCode != "200" {
		return a, raw, resp.StatusCode, fmt.Errorf("AIMS answered %s: %s", a.ResponseCode, a.message())
	}
	return a, raw, resp.StatusCode, nil
}

// token is the bearer token for an account: the one kept, refreshed, or
// signed in for anew.
func (c *SolumClient) token(ctx context.Context, a SolumAccount, renew bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	who := a.Base + "\x00" + a.Username + "\x00" + a.Password
	if c.signedIn != who {
		c.signedIn, c.access, c.refresh = who, "", ""
	}
	if c.access != "" && !renew {
		return c.access, nil
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	take := func(ans solumAnswer) error {
		if err := json.Unmarshal(ans.ResponseMessage, &tokens); err != nil || tokens.AccessToken == "" {
			return errors.New("AIMS gave no token")
		}
		c.access, c.refresh = tokens.AccessToken, tokens.RefreshToken
		return nil
	}
	if c.refresh != "" {
		ans, _, _, err := c.call(ctx, http.MethodPost, a.Base+"/common/api/v2/token/refresh", "", map[string]string{"refreshToken": c.refresh})
		if err == nil && take(ans) == nil {
			return c.access, nil
		}
	}
	c.access, c.refresh = "", ""
	ans, _, _, err := c.call(ctx, http.MethodPost, a.Base+"/common/api/v2/token", "", map[string]string{"username": a.Username, "password": a.Password})
	if errors.Is(err, errSolumAuth) {
		return "", errors.New("AIMS refused the username or password: set them under Admin › Displays")
	}
	if err != nil {
		return "", fmt.Errorf("signing in to AIMS: %v", err)
	}
	if err := take(ans); err != nil {
		return "", err
	}
	return c.access, nil
}

// authed makes a request with the token, signing in again once if AIMS
// refuses it.
func (c *SolumClient) authed(ctx context.Context, a SolumAccount, method, url string, body any) (solumAnswer, []byte, int, error) {
	for renew := false; ; renew = true {
		token, err := c.token(ctx, a, renew)
		if err != nil {
			return solumAnswer{}, nil, 0, err
		}
		ans, raw, code, err := c.call(ctx, method, url, token, body)
		if !errors.Is(err, errSolumAuth) || renew {
			if errors.Is(err, errSolumAuth) {
				err = errors.New("AIMS refused BAM!'s token even after signing in again")
			}
			return ans, raw, code, err
		}
	}
}

// PushSolum shows a PNG of a panel's size on a SoluM label: it checks the
// label is that size and in those inks, pushes the image without
// dithering, and waits a little for the label to show it. A label that
// has not shown it yet will, when it next hears from its gateway.
func (c *SolumClient) PushSolum(ctx context.Context, a SolumAccount, address string, panel Panel, png []byte) error {
	if a.Username == "" || a.Password == "" || a.Company == "" || a.Store == "" {
		return errors.New("the SoluM account is not set up: set its company, store, username and password under Admin › Displays")
	}
	at, err := parseSolumAddress(address)
	if err != nil {
		return err
	}
	base := strings.TrimRight(a.Base, "/")
	a.Base = base

	_, raw, _, err := c.authed(ctx, a, http.MethodGet, base+"/common/api/v2/common/labels/type/info?labelCode="+url.QueryEscape(at.Label), nil)
	if err != nil {
		return fmt.Errorf("reading label %s: %v", at.Label, err)
	}
	var info struct {
		Name          string `json:"name"`
		DisplayWidth  int    `json:"displayWidth"`
		DisplayHeight int    `json:"displayHeight"`
		Color         string `json:"color"`
		TotalPage     int    `json:"totalPage"`
	}
	// The label's type is the answer itself, or its message.
	_ = json.Unmarshal(raw, &info)
	if info.DisplayWidth == 0 {
		var ans solumAnswer
		_ = json.Unmarshal(raw, &ans)
		_ = json.Unmarshal(ans.ResponseMessage, &info)
	}
	if info.DisplayWidth == 0 {
		return fmt.Errorf("AIMS did not say label %s's size", at.Label)
	}
	inks, known := solumColors[strings.ToUpper(info.Color)]
	if info.DisplayWidth != panel.Width || info.DisplayHeight != panel.Height || !known || inks != panel.Inks() {
		return fmt.Errorf("label %s is %s, %d × %d in %s, where its model is %d × %d in %s: choose the model that matches it",
			at.Label, info.Name, info.DisplayWidth, info.DisplayHeight, info.Color, panel.Width, panel.Height, strings.ToUpper(string(panel.Inks())))
	}
	if info.TotalPage > 0 && at.Page > info.TotalPage {
		return fmt.Errorf("label %s has %d pages, not %d", at.Label, info.TotalPage, at.Page)
	}

	push := map[string]any{"labelCode": at.Label, "image": base64.StdEncoding.EncodeToString(png), "page": at.Page, "frontPage": at.Page, "dithering": false}
	q := url.Values{"company": {a.Company}, "store": {a.Store}, "cbId": {"yes"}}
	var ans solumAnswer
	for try := 1; ; try++ {
		var code int
		ans, _, code, err = c.authed(ctx, a, http.MethodPost, base+"/common/api/v2/common/labels/image?"+q.Encode(), push)
		if err == nil || (code < 500 && code != 0) || try >= solumTries {
			break
		}
		select {
		case <-time.After(solumPoll):
		case <-ctx.Done():
			return fmt.Errorf("%v, and the job was given up: %v", err, ctx.Err())
		}
	}
	if err != nil {
		return fmt.Errorf("pushing to label %s: %v", at.Label, err)
	}
	if ans.CustomBatchID == "" {
		return nil
	}

	// Whether it is shown: SUCCESS is, FAILED TO TRANSMIT will not be, and
	// the rest are still on their way.
	hq := url.Values{"company": {a.Company}, "store": {a.Store}, "labelCode": {at.Label}, "name": {ans.CustomBatchID}, "linkedProducts": {"ALL"}}
	pctx, cancel := context.WithTimeout(ctx, solumWait)
	defer cancel()
	for {
		select {
		case <-time.After(solumPoll):
		case <-pctx.Done():
			return nil
		}
		_, raw, _, err := c.authed(pctx, a, http.MethodGet, base+"/common/api/v2/common/articles/history/detail?"+hq.Encode(), nil)
		if err != nil {
			continue
		}
		switch solumResult(raw) {
		case "SUCCESS":
			return nil
		case "FAILED TO TRANSMIT":
			return fmt.Errorf("AIMS could not send the image to label %s: its gateway did not reach it", at.Label)
		}
	}
}

// solumResult is the result of a push in its history: the first given.
func solumResult(raw []byte) string {
	var h struct {
		UpdateHistoryList []struct {
			Result string `json:"result"`
		} `json:"updateHistoryList"`
		ResponseMessage *struct {
			UpdateHistoryList []struct {
				Result string `json:"result"`
			} `json:"updateHistoryList"`
		} `json:"responseMessage"`
	}
	_ = json.Unmarshal(raw, &h)
	list := h.UpdateHistoryList
	if len(list) == 0 && h.ResponseMessage != nil {
		list = h.ResponseMessage.UpdateHistoryList
	}
	for _, e := range list {
		if e.Result != "" {
			return strings.ToUpper(strings.TrimSpace(e.Result))
		}
	}
	return ""
}
