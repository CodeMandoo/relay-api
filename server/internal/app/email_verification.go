package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxEmailCodeAttempts = 5

type sendEmailCodeRequest struct {
	Email string `json:"email"`
}

func (a *App) sendRegisterEmailCode(c *gin.Context) {
	if !a.cfg.RequireEmailVerification {
		errorJSON(c, http.StatusForbidden, "email verification is disabled")
		return
	}
	var req sendEmailCodeRequest
	if !bindJSON(c, &req) {
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		errorJSON(c, http.StatusBadRequest, "invalid email")
		return
	}

	settings, err := a.getSettings()
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "settings not initialized")
		return
	}
	if !settings.OpenRegistration {
		errorJSON(c, http.StatusForbidden, "registration is closed")
		return
	}

	var existing int64
	if err := a.db.Model(&User{}).Where("email = ?", email).Count(&existing).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	if existing > 0 {
		errorJSON(c, http.StatusConflict, "email already registered")
		return
	}

	now := time.Now()
	var latest EmailVerificationCode
	if err := a.db.Where("email = ? AND purpose = ?", email, EmailCodePurposeRegister).
		Order("sent_at desc").
		First(&latest).Error; err == nil && now.Sub(latest.SentAt) < a.emailCodeCooldown() {
		errorJSON(c, http.StatusTooManyRequests, "verification code was sent recently")
		return
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}

	code, err := generateNumericCode(6)
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "generate verification code failed")
		return
	}
	record := EmailVerificationCode{
		Email:     email,
		Purpose:   EmailCodePurposeRegister,
		CodeHash:  a.emailCodeHash(email, EmailCodePurposeRegister, code),
		ExpiresAt: now.Add(a.emailCodeTTL()),
		SentAt:    now,
	}
	if err := a.db.Create(&record).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "save verification code failed")
		return
	}

	sent, err := a.sendVerificationEmail(email, code)
	if err != nil {
		_ = a.db.Delete(&record).Error
		errorJSON(c, http.StatusBadGateway, "send verification email failed")
		return
	}

	payload := gin.H{
		"email":           email,
		"expiresIn":       int(a.emailCodeTTL().Seconds()),
		"cooldownSeconds": int(a.emailCodeCooldown().Seconds()),
		"sent":            sent,
	}
	if !sent && a.cfg.EmailCodeDevMode {
		payload["devCode"] = code
	}
	c.JSON(http.StatusOK, gin.H{"data": payload})
}

func (a *App) verifyEmailCode(tx *gorm.DB, email string, purpose string, code string, now time.Time) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("email verification code is required")
	}
	var record EmailVerificationCode
	err := tx.Where("email = ? AND purpose = ? AND used_at IS NULL", email, purpose).
		Order("created_at desc").
		First(&record).Error
	if err != nil {
		return errors.New("invalid email verification code")
	}
	if record.ExpiresAt.Before(now) {
		return errors.New("email verification code expired")
	}
	if record.Attempts >= maxEmailCodeAttempts {
		return errors.New("too many email verification attempts")
	}
	if record.CodeHash != a.emailCodeHash(email, purpose, code) {
		_ = tx.Model(&record).UpdateColumn("attempts", gorm.Expr("attempts + ?", 1)).Error
		return errors.New("invalid email verification code")
	}
	return tx.Model(&record).Updates(map[string]any{"used_at": &now}).Error
}

func (a *App) sendVerificationEmail(to string, code string) (bool, error) {
	if strings.TrimSpace(a.cfg.SMTPHost) == "" {
		if a.cfg.EmailCodeDevMode {
			return false, nil
		}
		return false, errors.New("smtp is not configured")
	}
	subject := "注册邮箱验证码"
	body := fmt.Sprintf("您的注册验证码是：%s\n\n验证码将在 %d 分钟后过期。如果不是您本人操作，请忽略这封邮件。", code, int(a.emailCodeTTL().Minutes()))
	return true, a.sendMail(to, subject, body)
}

func (a *App) sendMail(to string, subject string, body string) error {
	host := strings.TrimSpace(a.cfg.SMTPHost)
	port := strings.TrimSpace(a.cfg.SMTPPort)
	if port == "" {
		port = "587"
	}
	from := strings.TrimSpace(a.cfg.SMTPFrom)
	if from == "" {
		from = strings.TrimSpace(a.cfg.SMTPUsername)
	}
	if from == "" {
		from = "noreply@relay.local"
	}

	var auth smtp.Auth
	username := strings.TrimSpace(a.cfg.SMTPUsername)
	if username != "" {
		auth = smtp.PlainAuth("", username, a.cfg.SMTPPassword, host)
	}

	message := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")
	return smtp.SendMail(net.JoinHostPort(host, port), auth, from, []string{to}, []byte(message))
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", errors.New("email is required")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", errors.New("invalid email")
	}
	return email, nil
}

func (a *App) emailCodeHash(email string, purpose string, code string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(email) + "|" + purpose + "|" + strings.TrimSpace(code) + "|" + a.cfg.JWTSecret))
	return hex.EncodeToString(sum[:])
}

func (a *App) emailCodeTTL() time.Duration {
	if a.cfg.EmailCodeTTL > 0 {
		return a.cfg.EmailCodeTTL
	}
	return 10 * time.Minute
}

func (a *App) emailCodeCooldown() time.Duration {
	if a.cfg.EmailCodeCooldown > 0 {
		return a.cfg.EmailCodeCooldown
	}
	return time.Minute
}

func generateNumericCode(length int) (string, error) {
	if length <= 0 {
		length = 6
	}
	var builder strings.Builder
	builder.Grow(length)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		builder.WriteByte(byte('0' + n.Int64()))
	}
	return builder.String(), nil
}
