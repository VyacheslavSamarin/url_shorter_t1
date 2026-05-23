package storage

import "errors"

var (
	ErrURLNotFound = errors.New("not found")
	ErrURLExists   = errors.New("url already exists")
	ErrUrlDeleted  = errors.New("url already deleted or not found")

	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("user already exists")

	ErrVerificationNotFound = errors.New("verification code not found")
)
