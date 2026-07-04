package service

import (
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"
)

func newUserServiceTest(t *testing.T) (*UserService, *db.Repository) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })
	repo := db.NewRepository(sqliteDB)
	return NewUserService(repo), repo
}

// pigateID resolves the seeded super_admin's id.
func pigateID(t *testing.T, repo *db.Repository) string {
	t.Helper()
	u, err := repo.GetUserByUsername("pigate")
	if err != nil || u == nil {
		t.Fatalf("seeded pigate not found: %v", err)
	}
	return u.ID
}

func TestCreateUserValidation(t *testing.T) {
	svc, _ := newUserServiceTest(t)

	cases := []struct {
		name string
		req  model.CreateUserRequest
	}{
		{"short username", model.CreateUserRequest{Username: "ab", Password: "password123", Role: model.RoleSuperAdmin}},
		{"bad char username", model.CreateUserRequest{Username: "bad name!", Password: "password123", Role: model.RoleSuperAdmin}},
		{"short password", model.CreateUserRequest{Username: "validuser", Password: "short", Role: model.RoleSuperAdmin}},
		{"bad role", model.CreateUserRequest{Username: "validuser", Password: "password123", Role: "root"}},
	}
	for _, c := range cases {
		if _, err := svc.Create(c.req); err == nil {
			t.Errorf("%s: expected validation error, got nil", c.name)
		}
	}
}

func TestCreateUserSuccessAndDuplicate(t *testing.T) {
	svc, _ := newUserServiceTest(t)

	u, err := svc.Create(model.CreateUserRequest{Username: "viewer", Password: "password123", Role: model.RoleAdminReadonly})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !u.IsInitial {
		t.Error("new user must have is_initial=true (forced first-login password change)")
	}
	if u.Status != model.StatusActive {
		t.Errorf("new user status = %q, want active", u.Status)
	}
	if u.Role != model.RoleAdminReadonly {
		t.Errorf("new user role = %q, want admin_readonly", u.Role)
	}
	if u.PasswordHash == "" {
		t.Error("password hash should be set")
	}

	// Duplicate username rejected.
	if _, err := svc.Create(model.CreateUserRequest{Username: "viewer", Password: "password123", Role: model.RoleSuperAdmin}); err == nil {
		t.Error("expected duplicate username error, got nil")
	}
}

func TestGuardCannotDeleteSelf(t *testing.T) {
	svc, repo := newUserServiceTest(t)
	// Add a second super_admin so the last-super guard doesn't mask the self guard.
	if _, err := svc.Create(model.CreateUserRequest{Username: "admin2", Password: "password123", Role: model.RoleSuperAdmin}); err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	if err := svc.Delete("pigate", pigateID(t, repo)); err == nil {
		t.Error("expected self-delete to be blocked")
	}
}

func TestGuardCannotDisableSelf(t *testing.T) {
	svc, repo := newUserServiceTest(t)
	if _, err := svc.Create(model.CreateUserRequest{Username: "admin2", Password: "password123", Role: model.RoleSuperAdmin}); err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	if err := svc.Toggle("pigate", pigateID(t, repo)); err == nil {
		t.Error("expected self-disable to be blocked")
	}
}

func TestGuardCannotDemoteSelf(t *testing.T) {
	svc, repo := newUserServiceTest(t)
	if _, err := svc.Create(model.CreateUserRequest{Username: "admin2", Password: "password123", Role: model.RoleSuperAdmin}); err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	err := svc.Update("pigate", pigateID(t, repo), model.UpdateUserRequest{Role: model.RoleAdminReadonly})
	if err == nil {
		t.Error("expected self-demotion to be blocked")
	}
}

func TestGuardLastActiveSuperAdmin(t *testing.T) {
	svc, repo := newUserServiceTest(t)
	id := pigateID(t, repo)

	// Deleting the only active super_admin (as a different actor) must fail.
	if err := svc.Delete("other", id); err == nil {
		t.Error("expected last-super-admin delete to be blocked")
	}
	// Disabling the only active super_admin must fail.
	if err := svc.Toggle("other", id); err == nil {
		t.Error("expected last-super-admin disable to be blocked")
	}
	// Demoting the only active super_admin must fail.
	if err := svc.Update("other", id, model.UpdateUserRequest{Role: model.RoleAdminReadonly}); err == nil {
		t.Error("expected last-super-admin demotion to be blocked")
	}
}

func TestToggleBackAndForth(t *testing.T) {
	svc, repo := newUserServiceTest(t)
	u, err := svc.Create(model.CreateUserRequest{Username: "viewer", Password: "password123", Role: model.RoleAdminReadonly})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := svc.Toggle("pigate", u.ID); err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	got, _ := repo.GetUserByID(u.ID)
	if got.Status != model.StatusDisabled {
		t.Errorf("after first toggle status = %q, want disabled", got.Status)
	}

	if err := svc.Toggle("pigate", u.ID); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	got, _ = repo.GetUserByID(u.ID)
	if got.Status != model.StatusActive {
		t.Errorf("after second toggle status = %q, want active", got.Status)
	}
}

func TestUpdateRoleAndPasswordReset(t *testing.T) {
	svc, repo := newUserServiceTest(t)
	u, err := svc.Create(model.CreateUserRequest{Username: "viewer", Password: "password123", Role: model.RoleAdminReadonly})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Clear is_initial to simulate the user having already set their password.
	if err := repo.ChangePassword("viewer", u.PasswordHash); err != nil {
		t.Fatalf("clear is_initial failed: %v", err)
	}

	// Promote to super_admin and reset password in one update.
	newPass := "newpassword456"
	if err := svc.Update("pigate", u.ID, model.UpdateUserRequest{Role: model.RoleSuperAdmin, Password: &newPass}); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got, _ := repo.GetUserByID(u.ID)
	if got.Role != model.RoleSuperAdmin {
		t.Errorf("role = %q, want super_admin", got.Role)
	}
	if !got.IsInitial {
		t.Error("password reset must set is_initial=true")
	}
}

func TestUpdatePasswordTooShort(t *testing.T) {
	svc, repo := newUserServiceTest(t)
	u, err := svc.Create(model.CreateUserRequest{Username: "viewer", Password: "password123", Role: model.RoleAdminReadonly})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	short := "x"
	if err := svc.Update("pigate", u.ID, model.UpdateUserRequest{Role: model.RoleAdminReadonly, Password: &short}); err == nil {
		t.Error("expected short password reset to be rejected")
	}
	_ = repo
}

func TestUpdateUserNotFound(t *testing.T) {
	svc, _ := newUserServiceTest(t)
	err := svc.Update("pigate", "user-nonexistent", model.UpdateUserRequest{Role: model.RoleAdminReadonly})
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}
