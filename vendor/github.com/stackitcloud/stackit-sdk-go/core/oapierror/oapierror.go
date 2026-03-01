package oapierror

import (
	"fmt"
	"reflect"
	"strings"
)

var (
	// When a response has a bad status, this limits the number of characters that are shown from the response Body
	ApiErrorMaxCharacterLimit = 500
)

// GenericOpenAPIError Provides access to the Body, errorMessage and Model on returned Errors.
type GenericOpenAPIError struct {
	StatusCode   int
	Body         []byte
	ErrorMessage string
	Model        interface{}
}

func NewError(code int, status string) *GenericOpenAPIError {
	return &GenericOpenAPIError{
		StatusCode:   code,
		ErrorMessage: status,
		Model:        map[string]any{},
	}
}

func NewErrorWithBody(code int, status string, body []byte, model any) *GenericOpenAPIError {
	return &GenericOpenAPIError{
		StatusCode:   code,
		ErrorMessage: status,
		Body:         body,
		Model:        model,
	}
}

// Error returns non-empty string if there was an errorMessage.
func (e GenericOpenAPIError) Error() string {
	// Prevent panic in case of negative value
	if ApiErrorMaxCharacterLimit < 0 {
		ApiErrorMaxCharacterLimit = 500
	}

	if len(e.Body) <= ApiErrorMaxCharacterLimit {
		return fmt.Sprintf("%s, status code %d, Body: %s\n", e.ErrorMessage, e.StatusCode, string(e.Body))
	}
	indexStart := ApiErrorMaxCharacterLimit / 2
	indexEnd := len(e.Body) - ApiErrorMaxCharacterLimit/2
	numberTruncatedCharacters := indexEnd - indexStart
	return fmt.Sprintf(
		"%s, status code %d, Body: %s [...truncated %d characters...] %s",
		e.ErrorMessage,
		e.StatusCode,
		string(e.Body[:indexStart]),
		numberTruncatedCharacters,
		string(e.Body[indexEnd:]),
	)
}

// StatusCode returns the status code of the response
func (e GenericOpenAPIError) GetStatusCode() int {
	return e.StatusCode
}

// Body returns the raw bytes of the response
func (e GenericOpenAPIError) GetBody() []byte {
	return e.Body
}

// Model returns the unpacked Model of the errorMessage
func (e GenericOpenAPIError) GetModel() interface{} {
	return e.Model
}

// Format errorMessage message using title and detail when Model implements rfc7807
func FormatErrorMessage(status string, v interface{}) string {
	str := ""
	metaValue := reflect.ValueOf(v).Elem()
	switch metaValue.Kind() {
	case reflect.Map:
		return status
	case reflect.Struct:
		field := metaValue.FieldByName("Title")
		if field != (reflect.Value{}) {
			str = fmt.Sprintf("%s", field.Interface())
		}
		field = metaValue.FieldByName("Detail")
		if field != (reflect.Value{}) {
			str = fmt.Sprintf("%s (%s)", str, field.Interface())
		}

		// status title (detail)
		return strings.TrimSpace(fmt.Sprintf("%s %s", status, str))
	}
	return status
}
