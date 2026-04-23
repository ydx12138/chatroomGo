package mysql

import (
	"database/sql"
	"errors"
	"testing"

	driver "github.com/go-sql-driver/mysql"
)

func TestIsDuplicateNameError(t *testing.T) {
	err := &driver.MySQLError{Number: 1062, Message: "Duplicate entry"}
	if !isDuplicateNameError(err) {
		t.Fatalf("expected duplicate-name error to be detected")
	}
	if isDuplicateNameError(errors.New("plain error")) {
		t.Fatalf("plain error should not be treated as duplicate")
	}
}

func TestEvaluateLoginResult(t *testing.T) {
	tests := []struct {
		name         string
		queryErr     error
		storedPass   string
		inputPass    string
		wantResult   LoginResult
		wantHasError bool
	}{
		{
			name:       "user not found",
			queryErr:   sql.ErrNoRows,
			inputPass:  "123456",
			wantResult: LoginUserNotFound,
		},
		{
			name:       "password incorrect",
			storedPass: "654321",
			inputPass:  "123456",
			wantResult: LoginPasswordIncorrect,
		},
		{
			name:       "login success",
			storedPass: "123456",
			inputPass:  "123456",
			wantResult: LoginSuccess,
		},
		{
			name:         "query failure",
			queryErr:     errors.New("boom"),
			inputPass:    "123456",
			wantResult:   LoginDBError,
			wantHasError: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result, err := evaluateLoginResult(tc.storedPass, tc.inputPass, tc.queryErr)
			if result != tc.wantResult {
				t.Fatalf("expected result %v, got %v", tc.wantResult, result)
			}
			if tc.wantHasError && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantHasError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
