package gritsd

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
)

// Validation patterns (with length constraints embedded)
var validationPatterns = map[string]*regexp.Regexp{
	"blobAddr":     regexp.MustCompile(`^Qm[1-9A-HJ-NP-Za-km-z]{44}$`),
	"relativePath": regexp.MustCompile(`^([^\x00/]([^\x00]*[^\x00/])?)?$`), // Max 4096 checked separately for performance
	"volumeName":   regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,254}$`), // Max 255 chars total
	"base64":       regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`),
	"hostname":     regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`), // Max 253 via DNS label rules
}

// Validate checks if a string matches the specified validation type
// Returns true if valid, false otherwise
func Validate(validationType string, value string) bool {
	// Special case: empty strings
	if value == "" {
		// Empty is valid for relativePath and base64, invalid for others
		return validationType == "relativePath" || validationType == "base64"
	}
	
	// Special cases for relative path
	if validationType == "relativePath" {
		if len(value) > 4096 {
			return false
		}
		// We could check for .. or // here; it shouldn't matter because we're not in the filesystem, but it
		// would be safer
	}
	
	pattern, exists := validationPatterns[validationType]
	if !exists {
		fmt.Printf("Warning: unknown validation type '%s'\n", validationType)
		return false
	}
	return pattern.MatchString(value)
}

// ValidateStruct validates all fields in a struct based on their validate tags
// Returns error with details about first validation failure
func ValidateStruct(s interface{}) error {
	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	
	if v.Kind() != reflect.Struct {
		return nil // Not a struct, nothing to validate
	}
	
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		
		validateTag := fieldType.Tag.Get("validate")
		if validateTag == "" {
			continue
		}
		
		// Only validate string fields
		if field.Kind() != reflect.String {
			continue
		}
		
		fieldValue := field.String()
		if !Validate(validateTag, fieldValue) {
			return fmt.Errorf("field %s failed %s validation: %q", fieldType.Name, validateTag, fieldValue)
		}
	}
	
	return nil
}

// UnmarshalAndValidate unmarshals JSON and validates the result
func UnmarshalAndValidate(data []byte, result interface{}) error {
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("unmarshal failed: %v", err)
	}
	
	// Handle slices - validate each element
	v := reflect.ValueOf(result)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i)
			if err := ValidateStruct(elem.Interface()); err != nil {
				return fmt.Errorf("element %d: %v", i, err)
			}
		}
		return nil
	}
	
	// Single struct
	return ValidateStruct(result)
}