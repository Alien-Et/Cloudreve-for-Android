package admin

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin/binding"
)

func TestUpsertUserServiceValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{name: "empty", email: "", wantErr: true},
		{name: "missing top level domain", email: "admin@test", wantErr: true},
		{name: "localhost domain", email: "user@localhost", wantErr: true},
		{name: "surrounding whitespace", email: " user@example.com ", wantErr: true},
		{name: "regular address", email: "admin@test.com"},
		{name: "subaddress", email: "user+tag@example.co.uk"},
		{name: "internationalized address", email: "用户@例子.公司"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &UpsertUserService{User: &ent.User{Email: test.email}}
			err := service.validateEmail()
			if test.wantErr {
				if err == nil {
					t.Fatal("expected email validation to fail")
				}
				var appErr serializer.AppError
				if !errors.As(err, &appErr) {
					t.Fatalf("expected AppError, got %T", err)
				}
				if appErr.Code != serializer.CodeParamErr {
					t.Fatalf("unexpected error code: %d", appErr.Code)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected email validation to pass: %v", err)
			}
		})
	}
}

func TestUpsertUserServiceValidateEmailRejectsMissingUser(t *testing.T) {
	service := &UpsertUserService{}
	err := service.validateEmail()
	if err == nil {
		t.Fatal("expected missing user validation to fail")
	}

	var appErr serializer.AppError
	if !errors.As(err, &appErr) || appErr.Code != serializer.CodeParamErr {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestUpsertUserServicePasswordBinding(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "empty update", password: ""},
		{name: "short update", password: "12345", wantErr: true},
		{name: "minimum length update", password: "123456"},
		{name: "maximum length", password: strings.Repeat("a", 128)},
		{name: "too long", password: strings.Repeat("a", 129), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &UpsertUserService{
				User:     &ent.User{Email: "admin@example.com"},
				Password: test.password,
			}
			err := binding.Validator.ValidateStruct(service)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected password validation to fail")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected password validation to pass: %v", err)
			}
		})
	}
}
