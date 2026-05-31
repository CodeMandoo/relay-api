package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type registerRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	Name       string `json:"name"`
	InviteCode string `json:"inviteCode"`
	EmailCode  string `json:"emailCode"`
}

func (a *App) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || strings.TrimSpace(req.Password) == "" {
		errorJSON(c, http.StatusBadRequest, "email and password are required")
		return
	}

	var user User
	if err := a.db.Where("email = ?", email).First(&user).Error; err != nil {
		errorJSON(c, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if user.Status != UserStatusNormal || !verifyPassword(user.PasswordHash, req.Password) {
		errorJSON(c, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if req.Role != "" && req.Role != user.Role && !(req.Role == RoleUser && user.Role == RoleAdmin) {
		errorJSON(c, http.StatusForbidden, "role mismatch")
		return
	}
	now := time.Now()
	_ = a.db.Model(&user).Update("last_login_at", now).Error
	user.LastLoginAt = &now
	a.respondAuth(c, user)
}

func (a *App) register(c *gin.Context) {
	var req registerRequest
	if !bindJSON(c, &req) {
		return
	}
	email, emailErr := normalizeEmail(req.Email)
	password := strings.TrimSpace(req.Password)
	name := strings.TrimSpace(req.Name)
	inviteCode := strings.ToUpper(strings.TrimSpace(req.InviteCode))
	if emailErr != nil || password == "" {
		errorJSON(c, http.StatusBadRequest, "email and password are required")
		return
	}
	if len(password) < 8 {
		errorJSON(c, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	var settings PlatformSettings
	if err := a.db.First(&settings).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "settings not initialized")
		return
	}
	if !settings.OpenRegistration {
		errorJSON(c, http.StatusForbidden, "registration is closed")
		return
	}
	if settings.RequireInviteCode && inviteCode == "" {
		errorJSON(c, http.StatusBadRequest, "invite code is required")
		return
	}
	if a.cfg.RequireEmailVerification && strings.TrimSpace(req.EmailCode) == "" {
		errorJSON(c, http.StatusBadRequest, "email verification code is required")
		return
	}

	hash, err := hashPassword(password)
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "password hashing failed")
		return
	}

	var created User
	err = a.db.Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&User{}).Where("email = ?", email).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return errors.New("email already registered")
		}

		var invite *InviteCode
		if settings.RequireInviteCode || inviteCode != "" {
			var row InviteCode
			if err := tx.Where("code = ?", inviteCode).First(&row).Error; err != nil {
				return errors.New("invalid invite code")
			}
			if invitePublicStatus(row, time.Now()) != "valid" {
				return errors.New("invite code is not valid")
			}
			invite = &row
		}

		if a.cfg.RequireEmailVerification {
			if err := a.verifyEmailCode(tx, email, EmailCodePurposeRegister, req.EmailCode, time.Now()); err != nil {
				return err
			}
		}

		if invite != nil {
			invite.UsedCount++
			if err := tx.Save(invite).Error; err != nil {
				return err
			}
			inviteCode = invite.Code
		}

		created = User{
			Email:        email,
			Name:         name,
			PasswordHash: hash,
			Role:         RoleUser,
			Status:       UserStatusNormal,
			InviteCode:   inviteCode,
			MonthlyQuota: int64(settings.DefaultUserBalance * 10_000),
			WeeklyQuota:  int64(settings.DefaultUserBalance * 2_500),
			Balance:      settings.DefaultUserBalance,
		}
		return tx.Create(&created).Error
	})
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	a.respondAuth(c, created)
}

func (a *App) refreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !bindJSON(c, &req) {
		return
	}
	claims, err := a.parseToken(req.RefreshToken, "refresh")
	if err != nil {
		errorJSON(c, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	var user User
	if err := a.db.First(&user, claims.UserID).Error; err != nil {
		errorJSON(c, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if user.Status != UserStatusNormal {
		errorJSON(c, http.StatusForbidden, "user disabled")
		return
	}
	a.respondAuth(c, user)
}

func (a *App) me(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": authUserDTO(user)})
}

func (a *App) publicSettings(c *gin.Context) {
	settings, err := a.getSettings()
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "settings not initialized")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"platformName":             settings.PlatformName,
			"supportEmail":             settings.SupportEmail,
			"openRegistration":         settings.OpenRegistration,
			"requireInviteCode":        settings.RequireInviteCode,
			"requireEmailVerification": a.cfg.RequireEmailVerification,
		},
	})
}

func (a *App) respondAuth(c *gin.Context, user User) {
	access, err := a.signToken(user, "access", a.cfg.AccessTTL)
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "sign access token failed")
		return
	}
	refresh, err := a.signToken(user, "refresh", a.cfg.RefreshTTL)
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "sign refresh token failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"accessToken":  access,
		"refreshToken": refresh,
		"user":         authUserDTO(user),
	})
}
