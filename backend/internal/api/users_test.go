package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"
	"pigate/internal/service"
)

// seedReadonlyUser creates an active admin_readonly account (is_initial cleared
// so it can be used directly) and returns a live session token for it.
func seedReadonlyUser(t *testing.T, repo *db.Repository, username string) string {
	t.Helper()
	u := model.User{
		ID:           "user-" + username,
		Username:     username,
		PasswordHash: "x",
		IsInitial:    false,
		Role:         model.RoleAdminReadonly,
		Status:       model.StatusActive,
	}
	if err := repo.CreateUser(u); err != nil {
		t.Fatalf("seed readonly user failed: %v", err)
	}
	token := "session_id_readonly_" + username
	AddSession(token, username)
	return token
}

func TestReadonlyUserBlockedFromMutations(t *testing.T) {
	handler, repo := setupTestServer(t)
	token := seedReadonlyUser(t, repo, "viewer")

	// GET is allowed for a read-only user.
	req := httptest.NewRequest("GET", "/api/interfaces", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("readonly GET /api/interfaces = %d, want 200", rec.Code)
	}

	// A mutation must be blocked with 403.
	req = httptest.NewRequest("POST", "/api/policies", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("readonly POST /api/policies = %d, want 403", rec.Code)
	}

	// Read-only users may still change their own password (allow-listed).
	req = httptest.NewRequest("PUT", "/api/system/password", bytes.NewBufferString(`{"currentPassword":"nope","newPassword":"whatever12"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Errorf("readonly PUT /api/system/password should not be 403 (allow-listed), got %d", rec.Code)
	}
}

func TestReadonlyUserCannotAccessUserAPI(t *testing.T) {
	handler, repo := setupTestServer(t)
	token := seedReadonlyUser(t, repo, "viewer")

	// Even GET /api/users must be forbidden for a read-only admin.
	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("readonly GET /api/users = %d, want 403", rec.Code)
	}
}

func TestSuperAdminUserCRUDViaAPI(t *testing.T) {
	handler, _ := setupTestServer(t)
	// The default seeded "pigate" session is super_admin/active.
	adminToken := "mock_session_id_test_token"

	// List: should include the seeded pigate account.
	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("super_admin GET /api/users = %d, want 200", rec.Code)
	}
	var users []model.User
	json.NewDecoder(rec.Body).Decode(&users)
	if len(users) < 1 {
		t.Fatal("expected at least the seeded pigate user")
	}
	// Ensure password hash never leaks in the JSON.
	if bytes.Contains(rec.Body.Bytes(), []byte("password_hash")) {
		t.Error("user list response leaked password_hash")
	}

	// Create a new read-only user.
	body, _ := json.Marshal(model.CreateUserRequest{Username: "newviewer", Password: "password123", Role: model.RoleAdminReadonly})
	req = httptest.NewRequest("POST", "/api/users", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("super_admin POST /api/users = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created model.User
	json.NewDecoder(rec.Body).Decode(&created)
	if created.ID == "" || !created.IsInitial {
		t.Errorf("created user malformed: %+v", created)
	}

	// Toggle the new user (disable).
	req = httptest.NewRequest("POST", "/api/users/"+created.ID+"/toggle", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("toggle user = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// Delete the new user.
	req = httptest.NewRequest("DELETE", "/api/users/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("delete user = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestDisabledUserSessionEvictedImmediately(t *testing.T) {
	handler, repo := setupTestServer(t)

	// Create an active super_admin "admin2" and a live session for it.
	svc := service.NewUserService(repo)
	if _, err := svc.Create(model.CreateUserRequest{Username: "admin2", Password: "password123", Role: model.RoleSuperAdmin}); err != nil {
		t.Fatalf("create admin2 failed: %v", err)
	}
	// Clear is_initial so the session is usable.
	u, _ := repo.GetUserByUsername("admin2")
	repo.ChangePassword("admin2", u.PasswordHash)
	token := "session_id_admin2"
	AddSession(token, "admin2")

	// Works before disabling.
	req := httptest.NewRequest("GET", "/api/interfaces", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin2 GET before disable = %d, want 200", rec.Code)
	}

	// Disable admin2 directly in the DB (simulating another super_admin action).
	repo.SetUserStatus(u.ID, model.StatusDisabled)

	// The lingering session must now be rejected on the very next request.
	req = httptest.NewRequest("GET", "/api/interfaces", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("disabled user session GET = %d, want 401", rec.Code)
	}
}
