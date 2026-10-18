// Package requests holds the validated request structs: one per input shape,
// each carrying its own z schema.
package requests

import "github.com/AlexGalhardo/vukem/z"

// Login is the login form.
type Login struct {
	Email    string `json:"email"    form:"email"`
	Password string `json:"password" form:"password"`
	Remember bool   `json:"remember" form:"remember"`
}

// LoginSchema validates Login.
var LoginSchema = z.Struct[Login](z.Shape{
	"email":    z.String().Trim().Email(),
	"password": z.String().Min(1),
	"remember": z.Bool().Optional(),
})

// Register is the registration form.
type Register struct {
	Name                 string `json:"name"                  form:"name"`
	Email                string `json:"email"                 form:"email"`
	Password             string `json:"password"              form:"password"`
	PasswordConfirmation string `json:"password_confirmation" form:"password_confirmation"`
}

// RegisterSchema validates Register (password rules live here).
var RegisterSchema = z.Struct[Register](z.Shape{
	"name":                  z.String().Trim().Min(2).Max(100),
	"email":                 z.String().Trim().Email().Max(255),
	"password":              z.String().Min(PasswordMinLength).Max(PasswordMaxLength),
	"password_confirmation": z.String(),
}).Refine(func(r Register) error {
	if err := ValidatePassword(r.Password); err != nil {
		return err
	}

	if r.Password != r.PasswordConfirmation {
		return z.FieldError("password_confirmation", "The password confirmation does not match.")
	}

	return nil
})

// ForgotPassword is the reset link request form.
type ForgotPassword struct {
	Email string `json:"email" form:"email"`
}

// ForgotPasswordSchema validates ForgotPassword.
var ForgotPasswordSchema = z.Struct[ForgotPassword](z.Shape{"email": z.String().Trim().Email()})

// ResetPassword is the new password form.
type ResetPassword struct {
	Token                string `json:"token"                 form:"token"`
	Email                string `json:"email"                 form:"email"`
	Password             string `json:"password"              form:"password"`
	PasswordConfirmation string `json:"password_confirmation" form:"password_confirmation"`
}

// ResetPasswordSchema validates ResetPassword.
var ResetPasswordSchema = z.Struct[ResetPassword](z.Shape{
	"token":                 z.String().Min(1),
	"email":                 z.String().Trim().Email(),
	"password":              z.String().Min(PasswordMinLength).Max(PasswordMaxLength),
	"password_confirmation": z.String(),
}).Refine(func(r ResetPassword) error {
	if err := ValidatePassword(r.Password); err != nil {
		return err
	}

	if r.Password != r.PasswordConfirmation {
		return z.FieldError("password_confirmation", "The password confirmation does not match.")
	}

	return nil
})

// UpdateProfile is the account-area profile form.
//
// The email address is deliberately absent. Changing it means re-verifying
// it, keeping the old address usable until the new one is confirmed, and
// deciding what happens to a half-finished change — a flow of its own, not a
// field on this form. Offering the input and silently not re-verifying would
// be worse than not offering it.
type UpdateProfile struct {
	Name string `json:"name" form:"name"`
}

// UpdateProfileSchema validates UpdateProfile.
var UpdateProfileSchema = z.Struct[UpdateProfile](z.Shape{
	"name": z.String().Trim().Min(2).Max(100),
})

// ChangePassword is the account-area password form.
type ChangePassword struct {
	CurrentPassword      string `json:"current_password"      form:"current_password"`
	Password             string `json:"password"              form:"password"`
	PasswordConfirmation string `json:"password_confirmation" form:"password_confirmation"`
}

// ChangePasswordSchema validates ChangePassword.
//
// The current password is required even though the session already proves who
// this is: a session can be a borrowed laptop, and a password change takes
// over the account.
var ChangePasswordSchema = z.Struct[ChangePassword](z.Shape{
	"current_password":      z.String().Min(1),
	"password":              z.String().Min(PasswordMinLength).Max(PasswordMaxLength),
	"password_confirmation": z.String(),
}).Refine(func(r ChangePassword) error {
	if err := ValidatePassword(r.Password); err != nil {
		return err
	}

	if r.Password != r.PasswordConfirmation {
		return z.FieldError("password_confirmation", "The password confirmation does not match.")
	}

	if r.Password == r.CurrentPassword {
		return z.FieldError("password", "The new password must be different from the current one.")
	}

	return nil
})

// CreateKey is the account-area API key form.
type CreateKey struct {
	DeviceName string   `json:"device_name" form:"device_name"`
	Abilities  []string `json:"abilities"   form:"abilities"`
}

// CreateKeySchema validates CreateKey. The abilities are checked against the
// closed set in the handler, which owns that list.
var CreateKeySchema = z.Struct[CreateKey](z.Shape{
	"device_name": z.String().Trim().Min(1).Max(100),
	"abilities":   z.Slice(z.String().Trim().Min(1).Max(64)).Min(1),
})
