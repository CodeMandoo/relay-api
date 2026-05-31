package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type authClaims struct {
	UserID    uint   `json:"userId"`
	Role      string `json:"role"`
	TokenType string `json:"tokenType"`
	jwt.RegisteredClaims
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (a *App) signToken(user User, tokenType string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := authClaims{
		UserID:    user.ID,
		Role:      user.Role,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.Email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.cfg.JWTSecret))
}

func (a *App) parseToken(tokenString string, expectedType string) (*authClaims, error) {
	tokenString = strings.TrimSpace(strings.TrimPrefix(tokenString, "Bearer "))
	if tokenString == "" {
		return nil, errors.New("missing token")
	}
	claims := &authClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(a.cfg.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	if expectedType != "" && claims.TokenType != expectedType {
		return nil, errors.New("invalid token type")
	}
	return claims, nil
}

func (a *App) requireAuth(roles ...string) gin.HandlerFunc {
	roleSet := map[string]bool{}
	for _, role := range roles {
		roleSet[role] = true
	}
	return func(c *gin.Context) {
		claims, err := a.parseToken(c.GetHeader("Authorization"), "access")
		if err != nil {
			errorJSON(c, http.StatusUnauthorized, "unauthorized")
			c.Abort()
			return
		}
		var user User
		if err := a.db.First(&user, claims.UserID).Error; err != nil {
			errorJSON(c, http.StatusUnauthorized, "unauthorized")
			c.Abort()
			return
		}
		if user.Status != UserStatusNormal {
			errorJSON(c, http.StatusForbidden, "user disabled")
			c.Abort()
			return
		}
		if len(roleSet) > 0 && !roleSet[user.Role] {
			errorJSON(c, http.StatusForbidden, "forbidden")
			c.Abort()
			return
		}
		c.Set("user", user)
		c.Next()
	}
}

func (a *App) requireAPIKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		secret := bearerOrHeaderKey(c)
		if secret == "" {
			errorJSON(c, http.StatusUnauthorized, "missing api key")
			c.Abort()
			return
		}
		var key APIKey
		if err := a.db.Where("key_hash = ?", hashKey(secret)).First(&key).Error; err != nil {
			errorJSON(c, http.StatusUnauthorized, "invalid api key")
			c.Abort()
			return
		}
		if key.Status != APIKeyStatusValid {
			errorJSON(c, http.StatusForbidden, "api key disabled")
			c.Abort()
			return
		}
		var user User
		if err := a.db.First(&user, key.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(c, http.StatusUnauthorized, "invalid api key")
			} else {
				errorJSON(c, http.StatusInternalServerError, "database error")
			}
			c.Abort()
			return
		}
		if user.Status != UserStatusNormal {
			errorJSON(c, http.StatusForbidden, "user disabled")
			c.Abort()
			return
		}
		c.Set("apiKey", key)
		c.Set("apiUser", user)
		c.Next()
	}
}

func bearerOrHeaderKey(c *gin.Context) string {
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if value := strings.TrimSpace(c.GetHeader("X-API-Key")); value != "" {
		return value
	}
	return strings.TrimSpace(c.Query("key"))
}

func currentUser(c *gin.Context) (User, bool) {
	value, ok := c.Get("user")
	if !ok {
		return User{}, false
	}
	user, ok := value.(User)
	return user, ok
}

func currentAPIIdentity(c *gin.Context) (User, APIKey, bool) {
	userValue, okUser := c.Get("apiUser")
	keyValue, okKey := c.Get("apiKey")
	user, userOK := userValue.(User)
	key, keyOK := keyValue.(APIKey)
	return user, key, okUser && okKey && userOK && keyOK
}
