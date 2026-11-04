package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateUserRequest defines the request body for creating a user account.
type CreateUserRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	IsActive *bool  `json:"is_active,omitempty"`
}

// UpdateUserStatusRequest defines the request body for modifying user status.
type UpdateUserStatusRequest struct {
	IsActive bool `json:"is_active"`
}

// handleListUsers lists all non-archived users.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers()
	if err != nil {
		writeServerError(w, err, "list users")
		return
	}
	assignments, err := s.store.UserRoleAssignments()
	if err != nil {
		writeServerError(w, err, "list user roles")
		return
	}
	delays, err := s.store.SignInDelays(time.Now())
	if err != nil {
		writeServerError(w, err, "list sign-in delays")
		return
	}
	twoFactor, err := s.store.HasFactors()
	if err != nil {
		writeServerError(w, err, "list second factors")
		return
	}
	list := make([]userWithRoles, 0, len(users))
	for _, u := range users {
		item := withRoles(u, assignments[u.ID])
		if until, ok := delays[auth.NormalizeEmail(u.Email)]; ok {
			item.SignInDelayedUntil = &until
		}
		item.TwoFactor = twoFactor[u.ID]
		list = append(list, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"users": list,
	})
}

// handleCreateUser provisions a new user account.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "Email and password are required")
		return
	}
	// Asset pages show people by name, never by email (docs/access.md).
	if strings.TrimSpace(req.Name) == "" {
		writeJSONError(w, http.StatusBadRequest, "A name is required")
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	user, err := s.store.CreateNamedUser(req.Email, req.Name, req.Password, isActive)
	if errors.Is(err, auth.ErrPasswordPolicy) || errors.Is(err, store.ErrInvalidUser) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if store.IsUniqueViolation(err) {
		writeJSONError(w, http.StatusConflict, "A user with this email already exists")
		return
	}
	if err != nil {
		writeStoreError(w, err, "create the user")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user": user,
	})
}

// handleUpdateUserStatus toggles an active/disabled user account with Anti-Lockout enforcement.
func (s *Server) handleUpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	var req UpdateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	err := s.store.UpdateUserStatus(id, req.IsActive)
	if errors.Is(err, store.ErrUserNotFound) {
		writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}
	if errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "update the user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "User status updated successfully",
	})
}

// handleDeleteUser archives/deletes a user account with Anti-Lockout enforcement.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	err := s.store.DeleteUser(id)
	if errors.Is(err, store.ErrUserNotFound) {
		writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}
	if errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "delete the user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "User deleted successfully",
	})
}

// userWithRoles is a user as the user list returns it: their roles, and
// the scopes each is narrowed to.
type userWithRoles struct {
	models.User
	RoleIDs []string                `json:"role_ids"`
	Roles   []models.RoleAssignment `json:"roles"`
	// SignInDelayedUntil is set while too many failed passwords hold back
	// their sign-ins (#246).
	SignInDelayedUntil *time.Time `json:"sign_in_delayed_until,omitempty"`
	// TwoFactor is whether they have a second factor (docs/access.md).
	TwoFactor bool `json:"two_factor"`
}

func withRoles(u models.User, assignments []models.RoleAssignment) userWithRoles {
	out := userWithRoles{User: u, RoleIDs: []string{}, Roles: assignments}
	if out.Roles == nil {
		out.Roles = []models.RoleAssignment{}
	}
	for _, a := range out.Roles {
		out.RoleIDs = append(out.RoleIDs, a.RoleID)
	}
	return out
}

// SetUserRolesRequest replaces a user's roles: Roles, each in every scope
// or narrowed to some, or RoleIDs, each in every scope, as before.
type SetUserRolesRequest struct {
	Roles   []models.RoleAssignment `json:"roles"`
	RoleIDs []string                `json:"role_ids"`
}

// assignments are the roles a request gives, from Roles or else RoleIDs;
// false when it gives neither.
func (req SetUserRolesRequest) assignments() ([]models.RoleAssignment, bool) {
	switch {
	case req.Roles != nil:
		return req.Roles, true
	case req.RoleIDs != nil:
		return models.Unrestricted(req.RoleIDs), true
	}
	return nil, false
}

// handleListRoles lists roles with their permissions: GET /api/v1/roles
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.store.ListRoles()
	if err != nil {
		writeServerError(w, err, "list roles")
		return
	}
	if roles == nil {
		roles = []models.RoleWithPermissions{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// handleSetUserRoles replaces a user's roles, each in every scope or
// narrowed to some, under the anti-lockout guard: PUT /api/v1/users/{id}/roles
func (s *Server) handleSetUserRoles(w http.ResponseWriter, r *http.Request) {
	var req SetUserRolesRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	assignments, given := req.assignments()
	if err != nil || !given {
		writeJSONError(w, http.StatusBadRequest, "roles is required: a list of {role_id, scope_ids}, or role_ids")
		return
	}
	err = s.store.SetUserRoleAssignments(r.PathValue("id"), assignments)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case errors.Is(err, store.ErrRoleNotFound), errors.Is(err, store.ErrScopeNotFound),
		errors.Is(err, store.ErrInvalidAssignment), errors.Is(err, rbac.ErrAntiLockoutTriggered):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeStoreError(w, err, "set the roles")
	default:
		all, err := s.store.UserRoleAssignments()
		if err != nil {
			writeServerError(w, err, "read roles")
			return
		}
		u := withRoles(models.User{}, all[r.PathValue("id")])
		writeJSON(w, http.StatusOK, map[string]any{"role_ids": u.RoleIDs, "roles": u.Roles})
	}
}

// SetNameRequest sets a user's display name; empty shows their email.
type SetNameRequest struct {
	Name *string `json:"name"`
}

// handleSetUserName renames a user: PUT /api/v1/users/{id}/name
func (s *Server) handleSetUserName(w http.ResponseWriter, r *http.Request) {
	s.setUserName(w, r, r.PathValue("id"))
}

// handleSetOwnName renames the signed-in user: PUT /api/v1/auth/name
func (s *Server) handleSetOwnName(w http.ResponseWriter, r *http.Request) {
	s.setUserName(w, r, CurrentUser(r.Context()).ID)
}

func (s *Server) setUserName(w http.ResponseWriter, r *http.Request, userID string) {
	var req SetNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		writeJSONError(w, http.StatusBadRequest, "A name is required")
		return
	}
	user, err := s.store.SetUserName(userID, *req.Name)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case errors.Is(err, store.ErrInvalidUser):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeStoreError(w, err, "rename the user")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"user": user})
	}
}

// SetThemeRequest sets how the web interface looks to the signed-in user.
type SetThemeRequest struct {
	Theme *string `json:"theme"`
}

// handleSetOwnTheme sets the signed-in user's theme, "light", "dark" or
// "system" to follow their system's setting: PUT /api/v1/auth/theme
func (s *Server) handleSetOwnTheme(w http.ResponseWriter, r *http.Request) {
	var req SetThemeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Theme == nil {
		writeJSONError(w, http.StatusBadRequest, "A theme is required: system, light or dark")
		return
	}
	user, err := s.store.SetUserTheme(CurrentUser(r.Context()).ID, *req.Theme)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case errors.Is(err, store.ErrInvalidUser):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeStoreError(w, err, "set the theme")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"user": user})
	}
}

// EmailNotificationsRequest turns email sent to one's roles on or off.
type EmailNotificationsRequest struct {
	Notifications *bool `json:"notifications"`
}

// handleSetOwnEmailNotifications sets whether email sent to a role the
// signed-in person holds reaches them: PUT /api/v1/auth/email with
// {"notifications": false}. It answers {"user"}.
func (s *Server) handleSetOwnEmailNotifications(w http.ResponseWriter, r *http.Request) {
	var req EmailNotificationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Notifications == nil {
		writeJSONError(w, http.StatusBadRequest, "notifications is required: true or false")
		return
	}
	user, err := s.store.SetUserEmailNotifications(CurrentUser(r.Context()).ID, *req.Notifications)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case err != nil:
		writeStoreError(w, err, "set email notifications")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"user": user})
	}
}

// SetPasswordRequest sets someone's password for them.
type SetPasswordRequest struct {
	Password string `json:"password"`
}

// handleSetUserPassword sets a user's password, for someone who has
// forgotten theirs, and signs them out everywhere (but for the session
// making the change, when it is their own):
// PUT /api/v1/users/{id}/password
func (s *Server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	var req SetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	keep := ""
	if sess := CurrentSession(r.Context()); sess != nil && CurrentUser(r.Context()).ID == r.PathValue("id") {
		keep = sess.ID
	}
	err := s.store.SetPassword(r.PathValue("id"), req.Password, keep)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case errors.Is(err, auth.ErrPasswordPolicy):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeServerError(w, err, "set the password")
	default:
		// A new password is a fresh start: guesses at the old one no
		// longer hold them back.
		if err := s.clearSignInDelay(r.PathValue("id")); err != nil {
			writeServerError(w, err, "clear the sign-in delay")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "Password set"})
	}
}

// handleClearSignInDelay forgets someone's failed sign-ins, so they may try
// again at once (#246): DELETE /api/v1/users/{id}/sign-in-delay
func (s *Server) handleClearSignInDelay(w http.ResponseWriter, r *http.Request) {
	err := s.clearSignInDelay(r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case err != nil:
		writeServerError(w, err, "clear the sign-in delay")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"message": "They can sign in again at once"})
	}
}

func (s *Server) clearSignInDelay(id string) error {
	u, err := s.store.GetUserByID(id)
	if err != nil {
		return err
	}
	return s.store.ClearSignInFailures(u.Email)
}

// RoleRequest creates or edits a role.
type RoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	// RequiresTwoFactor, when given, sets whether holders must sign in with
	// a second factor; left out, a change keeps what the role had.
	RequiresTwoFactor *bool `json:"requires_two_factor,omitempty"`
}

func (req RoleRequest) input() store.RoleInput {
	return store.RoleInput{Name: req.Name, Description: req.Description, Permissions: req.Permissions, RequiresTwoFactor: req.RequiresTwoFactor}
}

// handleCreateRole creates a role: POST /api/v1/roles
func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req RoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	role, err := s.store.CreateRole(req.input())
	if err != nil {
		writeRoleError(w, err, "create the role")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"role": role})
}

// handleUpdateRole renames a role and replaces its permissions:
// PUT /api/v1/roles/{id}
func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	var req RoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	role, err := s.actingStore(r).UpdateRole(r.PathValue("id"), req.input())
	if err != nil {
		writeRoleError(w, err, "update the role")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"role": role})
}

// RoleTwoFactorRequest sets whether a role requires two-factor sign-in.
type RoleTwoFactorRequest struct {
	Required *bool `json:"required"`
}

// handleSetRoleTwoFactor sets whether a role's holders must sign in with a
// second factor: PUT /api/v1/roles/{id}/two-factor. It is how the admin
// role's is turned off, which is recorded in history.
func (s *Server) handleSetRoleTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req RoleTwoFactorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Required == nil {
		writeJSONError(w, http.StatusBadRequest, "Give required, true or false")
		return
	}
	role, err := s.actingStore(r).SetRoleTwoFactor(r.PathValue("id"), *req.Required)
	if err != nil {
		writeRoleError(w, err, "change the role")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"role": role})
}

// handleDeleteRole deletes a role, taking it from everyone who holds it:
// DELETE /api/v1/roles/{id}
func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRole(r.PathValue("id")); err != nil {
		writeRoleError(w, err, "delete the role")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Role deleted"})
}

func writeRoleError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrRoleNotFound):
		writeJSONError(w, http.StatusNotFound, "Role not found")
	case errors.Is(err, store.ErrRoleProtected), errors.Is(err, store.ErrPublicRole):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, rbac.ErrAntiLockoutTriggered):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case store.IsUniqueViolation(err):
		writeJSONError(w, http.StatusConflict, "Another role already has this name")
	case errors.Is(err, store.ErrInvalidRole):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}
