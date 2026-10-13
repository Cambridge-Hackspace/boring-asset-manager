package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestPhase2FieldsCategoriesAssetsAPIWorkflow(t *testing.T) {
	srv, admin, _ := setupTestServer(t)

	// Admin session
	sess, err := srv.store.CreateSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	adminCookie := &http.Cookie{Name: SessionCookieName, Value: sess.ID}

	// ==========================================
	// 1. FIELDS API
	// ==========================================

	// 1.1 Create select field
	createSelectBody, _ := json.Marshal(CreateFieldRequest{
		Key:            "voltage_rating",
		Label:          "Voltage Rating",
		ValidationType: fields.TypeSelect,
		Options: json.RawMessage(`{
			"choices": [
				{"label": "110V", "value": "110v"},
				{"label": "230V", "value": "230v"}
			],
			"default": "230v"
		}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/fields", bytes.NewReader(createSelectBody))
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for field, got %d: %s", rec.Code, rec.Body.String())
	}
	var fSelectResp struct {
		Field models.Field `json:"field"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&fSelectResp)
	fieldVoltageID := fSelectResp.Field.ID

	// 1.2 Create text field
	createTextBody, _ := json.Marshal(CreateFieldRequest{
		Key:            "serial_num",
		Label:          "Serial Number",
		ValidationType: fields.TypeText,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/fields", bytes.NewReader(createTextBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for text field, got %d", rec.Code)
	}
	var fTextResp struct {
		Field models.Field `json:"field"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&fTextResp)
	fieldSerialID := fTextResp.Field.ID

	// 1.3 List fields
	req = httptest.NewRequest(http.MethodGet, "/api/v1/fields", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK listing fields, got %d", rec.Code)
	}

	// ==========================================
	// 2. CATEGORIES API
	// ==========================================

	// 2.1 Create Root Category
	createCatBody, _ := json.Marshal(CreateCategoryRequest{
		Name:     "Machinery",
		ColorHex: "#ef4444",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/categories", bytes.NewReader(createCatBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for category, got %d: %s", rec.Code, rec.Body.String())
	}
	var catResp struct {
		Category models.Category `json:"category"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&catResp)
	catMachineryID := catResp.Category.ID

	// 2.2 Create Child Category under Machinery
	createChildBody, _ := json.Marshal(CreateCategoryRequest{
		Name:     "CNC Mills",
		ParentID: &catMachineryID,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/categories", bytes.NewReader(createChildBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for child category, got %d", rec.Code)
	}
	var childResp struct {
		Category models.Category `json:"category"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&childResp)
	catCNCID := childResp.Category.ID

	// 2.3 Cycle Detection via API: Update Machinery's parent to CNC Mills -> 400 Bad Request
	badUpdateBody, _ := json.Marshal(UpdateCategoryRequest{
		Name:     "Machinery",
		ParentID: &catCNCID,
	})
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/categories/%s", catMachineryID), bytes.NewReader(badUpdateBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on cycle detection, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2.4 Assign serial_num (Required) to Machinery
	assignBody1, _ := json.Marshal(AssignCategoryFieldRequest{
		FieldID:    fieldSerialID,
		IsRequired: true,
	})
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/categories/%s/fields", catMachineryID), bytes.NewReader(assignBody1))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 assigning field to category, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2.5 Assign voltage_rating (Optional) to CNC Mills
	assignBody2, _ := json.Marshal(AssignCategoryFieldRequest{
		FieldID:    fieldVoltageID,
		IsRequired: false,
	})
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/categories/%s/fields", catCNCID), bytes.NewReader(assignBody2))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 assigning field, got %d", rec.Code)
	}

	// 2.6 Verify Effective Inherited Fields for CNC Mills (should inherit serial_num as Required)
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/categories/%s/fields", catCNCID), nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 getting effective fields, got %d", rec.Code)
	}
	var effFieldsResp struct {
		Fields []models.EffectiveField `json:"fields"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&effFieldsResp)
	if len(effFieldsResp.Fields) != 2 {
		t.Fatalf("expected 2 effective fields, got %d", len(effFieldsResp.Fields))
	}

	// ==========================================
	// 3. ASSETS API
	// ==========================================

	// 3.1 Missing required field serial_num -> 400 Bad Request
	badAssetBody, _ := json.Marshal(CreateAssetRequest{
		Name:        "Haas Mini Mill",
		CategoryIDs: []string{catCNCID},
		FieldValues: map[string]string{
			"voltage_rating": "230v",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(badAssetBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for missing required field, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3.2 Invalid select value -> 400 Bad Request
	badSelectAssetBody, _ := json.Marshal(CreateAssetRequest{
		Name:        "Haas Mini Mill",
		CategoryIDs: []string{catCNCID},
		FieldValues: map[string]string{
			"serial_num":     "HAAS-12345",
			"voltage_rating": "480v", // invalid choice
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(badSelectAssetBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid select option, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3.3 Create valid asset with generator keys
	goodAssetBody, _ := json.Marshal(CreateAssetRequest{
		Name:        "Haas Mini Mill",
		CategoryIDs: []string{catCNCID},
		GeneratorKeys: []models.AssetGeneratorKey{
			{GeneratorType: generators.TypeMicroQR, AssignedCode: "HAAS-01"},
			{GeneratorType: generators.TypeCode128, AssignedCode: "CNC-HAAS-01"},
		},
		FieldValues: map[string]string{
			"serial_num":     "HAAS-12345",
			"voltage_rating": "230v",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(goodAssetBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for asset, got %d: %s", rec.Code, rec.Body.String())
	}
	var assetResp struct {
		Asset models.AssetWithDetails `json:"asset"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&assetResp)
	assetID := assetResp.Asset.Asset.ID

	// 3.4 Duplicate Generator Code -> 409 Conflict
	dupCodeBody, _ := json.Marshal(CreateAssetRequest{
		Name: "Another Mill",
		GeneratorKeys: []models.AssetGeneratorKey{
			{GeneratorType: generators.TypeMicroQR, AssignedCode: "HAAS-01"}, // duplicate!
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(dupCodeBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate generator code, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3.5 GET /api/v1/assets/{id}
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/assets/%s", assetID), nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 querying asset, got %d", rec.Code)
	}

	// 3.6 Search assets
	req = httptest.NewRequest(http.MethodGet, "/api/v1/assets?search=HAAS-01", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 searching asset, got %d", rec.Code)
	}
	var searchResp struct {
		Assets []models.AssetWithDetails `json:"assets"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&searchResp)
	if len(searchResp.Assets) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(searchResp.Assets))
	}

	// 3.7 Delete Asset (Soft Delete)
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/assets/%s", assetID), nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 deleting asset, got %d", rec.Code)
	}
}
