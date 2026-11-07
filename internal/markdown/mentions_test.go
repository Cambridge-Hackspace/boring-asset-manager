package markdown

import (
	"reflect"
	"strings"
	"testing"
)

const (
	ada = "0a0a0a0a-0000-4000-8000-00000000000a"
	bob = "0b0b0b0b-0000-4000-8000-00000000000b"
)

// TestMentionsRenderTheNameNow: a mention is rendered with the name names
// gives for the ID, so a renamed person's mentions read with their new
// name, escaped; without a name it reads as someone.
func TestMentionsRenderTheNameNow(t *testing.T) {
	names := map[string]string{ada: "Ada", bob: `<script>alert(1)</script> & "Bob"`}
	lookup := func(id string) string { return names[id] }
	in := "Hi " + Mention(ada) + ", ask " + Mention(strings.ToUpper(bob)) + "."
	got := RenderWith(in, lookup)
	if !strings.Contains(got, `<span class="mention">@Ada</span>`) {
		t.Errorf("Ada: %s", got)
	}
	if strings.Contains(got, "<script") || !strings.Contains(got, `<span class="mention">@&lt;script&gt;alert(1)&lt;/script&gt; &amp; &#34;Bob&#34;</span>`) {
		t.Errorf("Bob, escaped: %s", got)
	}
	names[ada] = "Ada Lovelace"
	if got := RenderWith(in, lookup); !strings.Contains(got, "@Ada Lovelace") {
		t.Errorf("renamed: %s", got)
	}
	if got := Render(in); strings.Count(got, `<span class="mention">@someone</span>`) != 2 {
		t.Errorf("without names: %s", got)
	}
}

// TestMentionsAreNotInCode: <@id> in code is text, not a mention, and a
// span written in the Markdown never passes, mention class or not.
func TestMentionsAreNotInCode(t *testing.T) {
	in := "`" + Mention(ada) + "`\n\n```\n" + Mention(bob) + "\n```\n\n" + `<span class="mention">@Mallory</span>`
	got := RenderWith(in, func(string) string { return "X" })
	if strings.Contains(got, "@X") || strings.Contains(got, "Mallory</span>") {
		t.Errorf("code or raw HTML became a mention: %s", got)
	}
	if m := Mentions(in); len(m) != 0 {
		t.Errorf("mentions in code: %v", m)
	}
}

// TestMentionsList: Mentions lists each person once, in order, by their
// ID in lower case, and nothing that is not a whole <@uuid>.
func TestMentionsList(t *testing.T) {
	in := Mention(bob) + " and " + Mention(strings.ToUpper(ada)) + ", again " + Mention(bob) + ", not <@nobody> nor <@" + ada + " >"
	if got := Mentions(in); !reflect.DeepEqual(got, []string{bob, ada}) {
		t.Errorf("Mentions = %v", got)
	}
}

// TestPlainMentions: plain text names each person, leaving code alone.
func TestPlainMentions(t *testing.T) {
	in := "Hi " + Mention(ada) + " and " + Mention(bob) + ": `" + Mention(ada) + "`"
	got := PlainMentions(in, func(id string) string {
		if id == ada {
			return "Ada"
		}
		return ""
	})
	if want := "Hi @Ada and @someone: `" + Mention(ada) + "`"; got != want {
		t.Errorf("PlainMentions = %q, want %q", got, want)
	}
	if got := PlainMentions("no one", nil); got != "no one" {
		t.Errorf("without mentions: %q", got)
	}
}
