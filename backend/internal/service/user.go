package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"

	"golang.org/x/crypto/bcrypt"

	"pigate/internal/db"
	"pigate/internal/model"
)

// usernameRegex mirrors the frontend validation: 3-32 chars, alphanumerics and
// underscore only. Enforced here so both the API and any future import path get
// consistent behavior regardless of the caller.
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

const minPasswordLength = 8

// ErrUserNotFound lets the API layer translate a missing target into HTTP 404
// while every other (validation / guard-rail) failure maps to 400.
var ErrUserNotFound = errors.New("ไม่พบผู้ใช้ที่ระบุ")

// UserService owns all business rules for the multi-user system. It is a pure
// DB feature: it never touches the kernel/OS layer. All guard rails live here
// (single source of truth) so they can't be bypassed by an individual handler.
type UserService struct {
	repo *db.Repository
}

func NewUserService(repo *db.Repository) *UserService {
	return &UserService{repo: repo}
}

// List returns every account (password hashes are never serialized).
func (s *UserService) List() ([]model.User, error) {
	return s.repo.GetUsers()
}

func validateRole(role string) error {
	if role != model.RoleSuperAdmin && role != model.RoleAdminReadonly {
		return fmt.Errorf("บทบาทไม่ถูกต้อง: %q", role)
	}
	return nil
}

func validateUsername(username string) error {
	if !usernameRegex.MatchString(username) {
		return errors.New("ชื่อผู้ใช้ต้องมี 3-32 ตัวอักษร ใช้ได้เฉพาะ a-z A-Z 0-9 และ _")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return fmt.Errorf("รหัสผ่านต้องมีอย่างน้อย %d ตัวอักษร", minPasswordLength)
	}
	return nil
}

func generateUserID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "user-" + hex.EncodeToString(b), nil
}

// Create validates and inserts a new user. New accounts always start with
// is_initial=1 so the person is forced to set their own password on first login.
func (s *UserService) Create(req model.CreateUserRequest) (*model.User, error) {
	if err := validateUsername(req.Username); err != nil {
		return nil, err
	}
	if err := validatePassword(req.Password); err != nil {
		return nil, err
	}
	if err := validateRole(req.Role); err != nil {
		return nil, err
	}

	exists, err := s.repo.UsernameExists(req.Username)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("ชื่อผู้ใช้ %q ถูกใช้งานแล้ว", req.Username)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		return nil, errors.New("ไม่สามารถเข้ารหัสรหัสผ่านได้")
	}

	id, err := generateUserID()
	if err != nil {
		return nil, errors.New("ไม่สามารถสร้างรหัสผู้ใช้ได้")
	}

	u := model.User{
		ID:           id,
		Username:     req.Username,
		PasswordHash: string(hash),
		IsInitial:    true,
		Role:         req.Role,
		Status:       model.StatusActive,
	}
	if err := s.repo.CreateUser(u); err != nil {
		return nil, err
	}

	return s.repo.GetUserByID(id)
}

// remainingActiveSuperAdmins computes how many active super_admins WOULD remain
// after a mutation on target, given whether target will still count as an active
// super_admin afterwards. This is the single check that prevents the system from
// ever being left without a usable super_admin.
func (s *UserService) remainingActiveSuperAdmins(target *model.User, willBeActiveSuperAdmin bool) (int, error) {
	count, err := s.repo.CountActiveSuperAdmins()
	if err != nil {
		return 0, err
	}
	currentlyCounts := target.Role == model.RoleSuperAdmin && target.Status == model.StatusActive
	if currentlyCounts {
		count--
	}
	if willBeActiveSuperAdmin {
		count++
	}
	return count, nil
}

// Update changes a user's role and, optionally, resets their password. A reset
// (Password != nil) forces the target to change it on next login.
func (s *UserService) Update(actorUsername, id string, req model.UpdateUserRequest) error {
	if err := validateRole(req.Role); err != nil {
		return err
	}

	target, err := s.repo.GetUserByID(id)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrUserNotFound
	}

	isSelf := target.Username == actorUsername

	// Guard: cannot demote yourself (avoid locking yourself out of admin).
	if isSelf && req.Role != target.Role && req.Role == model.RoleAdminReadonly {
		return errors.New("ไม่สามารถลดบทบาทของตัวเองได้")
	}

	// Guard: role change must not remove the last active super_admin.
	if req.Role != target.Role {
		willBeActiveSuper := req.Role == model.RoleSuperAdmin && target.Status == model.StatusActive
		remaining, err := s.remainingActiveSuperAdmins(target, willBeActiveSuper)
		if err != nil {
			return err
		}
		if remaining < 1 {
			return errors.New("ต้องมี super_admin ที่เปิดใช้งานอย่างน้อย 1 คนเสมอ")
		}
	}

	// Optional password reset.
	if req.Password != nil {
		if err := validatePassword(*req.Password); err != nil {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), 10)
		if err != nil {
			return errors.New("ไม่สามารถเข้ารหัสรหัสผ่านได้")
		}
		if err := s.repo.ResetUserPassword(id, string(hash)); err != nil {
			return err
		}
	}

	if req.Role != target.Role {
		if err := s.repo.UpdateUserRole(id, req.Role); err != nil {
			return err
		}
	}

	return nil
}

// Delete removes a user. Cannot delete yourself, and cannot remove the last
// active super_admin.
func (s *UserService) Delete(actorUsername, id string) error {
	target, err := s.repo.GetUserByID(id)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrUserNotFound
	}

	if target.Username == actorUsername {
		return errors.New("ไม่สามารถลบบัญชีของตัวเองได้")
	}

	remaining, err := s.remainingActiveSuperAdmins(target, false)
	if err != nil {
		return err
	}
	if remaining < 1 {
		return errors.New("ต้องมี super_admin ที่เปิดใช้งานอย่างน้อย 1 คนเสมอ")
	}

	return s.repo.DeleteUser(id)
}

// Toggle flips a user between active and disabled. Cannot disable yourself, and
// cannot disable the last active super_admin.
func (s *UserService) Toggle(actorUsername, id string) error {
	target, err := s.repo.GetUserByID(id)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrUserNotFound
	}

	newStatus := model.StatusDisabled
	if target.Status == model.StatusDisabled {
		newStatus = model.StatusActive
	}

	if newStatus == model.StatusDisabled {
		if target.Username == actorUsername {
			return errors.New("ไม่สามารถปิดใช้งานบัญชีของตัวเองได้")
		}
		willBeActiveSuper := target.Role == model.RoleSuperAdmin && false
		remaining, err := s.remainingActiveSuperAdmins(target, willBeActiveSuper)
		if err != nil {
			return err
		}
		if remaining < 1 {
			return errors.New("ต้องมี super_admin ที่เปิดใช้งานอย่างน้อย 1 คนเสมอ")
		}
	}

	return s.repo.SetUserStatus(id, newStatus)
}
