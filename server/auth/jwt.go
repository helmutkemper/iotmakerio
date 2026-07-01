// auth/jwt.go — JWT creation and verification for the IoTMaker API.
//
// JWT tokens are used for stateless API authentication (Authorization: Bearer …).
// Web UI uses session cookies (store/sessions.go) which are stateful and can be
// revoked instantly; JWT tokens are short-lived (1 hour) to limit exposure.
//
// Claims are minimal by design — only user ID, role, and expiry.
// Do not add sensitive fields (email, etc.) to JWT claims.
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTLifetime is how long a portal token remains valid.
// 7 days matches typical "stay logged in" expectations for a developer portal.
const JWTLifetime = 7 * 24 * time.Hour

// ControlTokenLifetime is how long a control panel token remains valid.
// Short-lived by design — the admin must re-authenticate after 1 hour of
// inactivity. Control tokens are only accepted on /api/control/v1/* routes.
const ControlTokenLifetime = 1 * time.Hour

// controlTokenIssuer identifies control panel tokens.
// ParseJWT rejects control tokens on portal routes and vice-versa.
const controlTokenIssuer = "iotmaker-control"

// portalTokenIssuer identifies standard portal tokens.
const portalTokenIssuer = "iotmaker"

// Claims holds the custom JWT payload.
type Claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// NewJWT creates a signed JWT for the given user.
// secret must be a non-empty string loaded from configuration.
func NewJWT(userID, role, secret string) (string, error) {
	if secret == "" {
		return "", errors.New("auth: jwt secret is empty")
	}

	now := time.Now().UTC()
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(JWTLifetime)),
			Issuer:    portalTokenIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// NewControlJWT creates a short-lived signed JWT for control panel access.
// Only users with admin role (or specific permissions) should receive this token.
// Control tokens are verified by ParseControlJWT — ParseJWT rejects them.
func NewControlJWT(userID, role, secret string) (string, error) {
	if secret == "" {
		return "", errors.New("auth: jwt secret is empty")
	}

	now := time.Now().UTC()
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ControlTokenLifetime)),
			Issuer:    controlTokenIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseControlJWT validates a control panel token.
// Rejects tokens issued for the portal (wrong issuer).
func ParseControlJWT(tokenStr, secret string) (*Claims, error) {
	claims, err := parseJWT(tokenStr, secret)
	if err != nil {
		return nil, err
	}
	if claims.Issuer != controlTokenIssuer {
		return nil, errors.New("auth: token is not a control panel token")
	}
	return claims, nil
}

// ParseJWT validates a portal token.
// Rejects control panel tokens (wrong issuer) so they cannot be used on
// portal routes even if they are otherwise valid.
func ParseJWT(tokenStr, secret string) (*Claims, error) {
	claims, err := parseJWT(tokenStr, secret)
	if err != nil {
		return nil, err
	}
	if claims.Issuer != portalTokenIssuer {
		return nil, errors.New("auth: token is not a portal token")
	}
	return claims, nil
}

// parseJWT is the shared implementation — verifies signature and expiry
// without checking the issuer. Called by ParseJWT and ParseControlJWT.
func parseJWT(tokenStr, secret string) (*Claims, error) {
	if secret == "" {
		return nil, errors.New("auth: jwt secret is empty")
	}

	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("auth: unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("auth: invalid token claims")
	}
	return claims, nil
}
