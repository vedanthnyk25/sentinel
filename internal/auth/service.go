package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid email or password")

type Service struct {
	db        *database.Queries
	jwtSecret []byte
}

func NewService(db *database.Queries, secretKey string) *Service {
	return &Service{
		db:        db,
		jwtSecret: []byte(secretKey),
	}
}

func (s *Service) RegisterUser(ctx context.Context, email, password string) (database.User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return database.User{}, err
	}

	ID := uuid.New()

	err = s.db.CreateUser(ctx, database.CreateUserParams{
		ID:           ID,
		Email:        email,
		PasswordHash: string(hashedPassword),
	})
	if err != nil {
		return database.User{}, err
	}

	return database.User{
		ID:    ID,
		Email: email,
	}, nil
}

func (s *Service) LoginUser(ctx context.Context, email, password string) (string, error) {
	user, err := s.db.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrInvalidCredentials
		}
		return "", err
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return "", ErrInvalidCredentials
	}

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID.String(),
		"exp":     time.Now().Add(24 * time.Hour).Unix(), // Token dies in 24 hours
	})

	tokenString, err := jwtToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}
