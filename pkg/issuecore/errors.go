package issuecore

import (
	"errors"
	"fmt"
)

var (
	ErrProviderAlreadyRegistered = errors.New("provider already registered")
	ErrProviderNotFound          = errors.New("provider not found")
	ErrProviderRequired          = errors.New("provider is required")
	ErrConfiguration             = errors.New("provider configuration error")
	ErrInvalidInput              = errors.New("invalid input")
	ErrResourceNotFound          = errors.New("resource not found")
	ErrNotImplemented            = errors.New("operation not implemented")
)

type OperationError struct {
	Code      string
	Provider  string
	Operation string
	Err       error
}

func (e *OperationError) Error() string {
	switch e.Code {
	case "provider_required":
		return fmt.Sprintf("provider is required for %s", e.Operation)
	case "provider_not_found":
		return fmt.Sprintf("provider %q is not registered for %s", e.Provider, e.Operation)
	case "configuration_error":
		return fmt.Sprintf("provider %q configuration for %s is invalid: %v", e.Provider, e.Operation, e.Err)
	case "provider_config_error":
		return fmt.Sprintf("provider %q configuration for %s is invalid: %v", e.Provider, e.Operation, e.Err)
	case "invalid_input":
		return fmt.Sprintf("provider %q rejected %s input: %v", e.Provider, e.Operation, e.Err)
	case "not_found":
		return fmt.Sprintf("provider %q could not find resource for %s: %v", e.Provider, e.Operation, e.Err)
	case "not_implemented":
		return fmt.Sprintf("provider %q does not implement %s yet", e.Provider, e.Operation)
	case "authentication_error":
		return fmt.Sprintf("provider %q authentication failed for %s: %v", e.Provider, e.Operation, e.Err)
	case "rate_limited":
		return fmt.Sprintf("provider %q rate limited %s: %v", e.Provider, e.Operation, e.Err)
	case "storage_error":
		return fmt.Sprintf("provider %q storage failed for %s: %v", e.Provider, e.Operation, e.Err)
	case "upstream_error":
		return fmt.Sprintf("provider %q upstream %s failed: %v", e.Provider, e.Operation, e.Err)
	default:
		if e.Provider == "" {
			return fmt.Sprintf("%s failed: %v", e.Operation, e.Err)
		}
		return fmt.Sprintf("provider %q %s failed: %v", e.Provider, e.Operation, e.Err)
	}
}

func (e *OperationError) Unwrap() error {
	return e.Err
}

func ProviderRequiredError(operation string) error {
	return &OperationError{
		Code:      "provider_required",
		Operation: operation,
		Err:       ErrProviderRequired,
	}
}

func ProviderLookupError(provider, operation string) error {
	return &OperationError{
		Code:      "provider_not_found",
		Provider:  provider,
		Operation: operation,
		Err:       ErrProviderNotFound,
	}
}

func NotImplemented(provider, operation string) error {
	return &OperationError{
		Code:      "not_implemented",
		Provider:  provider,
		Operation: operation,
		Err:       ErrNotImplemented,
	}
}

func ConfigurationError(provider, operation string, err error) error {
	return &OperationError{
		Code:      "configuration_error",
		Provider:  provider,
		Operation: operation,
		Err:       errors.Join(ErrConfiguration, err),
	}
}

func InvalidInput(provider, operation string, err error) error {
	return &OperationError{
		Code:      "invalid_input",
		Provider:  provider,
		Operation: operation,
		Err:       errors.Join(ErrInvalidInput, err),
	}
}

func ResourceNotFound(provider, operation string, err error) error {
	return &OperationError{
		Code:      "not_found",
		Provider:  provider,
		Operation: operation,
		Err:       errors.Join(ErrResourceNotFound, err),
	}
}
