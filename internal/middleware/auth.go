package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type contextKey string

const UserIDKey contextKey = "user_id"

func RequireAuth(secretKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract the Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			// Check if it's a Bearer token
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid Authorization format. Expected 'Bearer <token>'", http.StatusUnauthorized)
				return
			}

			tokenString := parts[1]

			// Parse and validate the JWT
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				// Validate the signing algorithm
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, http.ErrAbortHandler
				}
				return []byte(secretKey), nil
			})

			if err != nil || !token.Valid {
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Extract the user_id from the token claims
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, "Invalid token claims", http.StatusUnauthorized)
				return
			}

			userIDStr, ok := claims["user_id"].(string)
			if !ok {
				http.Error(w, "user_id not found in token", http.StatusUnauthorized)
				return
			}

			// Safely parse the UUID
			userID, err := uuid.Parse(userIDStr)
			if err != nil {
				http.Error(w, "Invalid user_id format in token", http.StatusUnauthorized)
				return
			}

			// INJECT THE USER ID INTO THE CONTEXT
			ctx := context.WithValue(r.Context(), UserIDKey, userID)

			// Pass the request to the next handler, using the new enriched context
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
