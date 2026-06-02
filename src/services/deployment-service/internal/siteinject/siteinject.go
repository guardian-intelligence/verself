package siteinject

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-service/siteconfig"
)

func Apply(job *api.Job, model siteconfig.Model) error {
	if job == nil {
		return fmt.Errorf("nomad job is nil")
	}
	replacements := model.TokenMap()
	seen := map[uintptr]bool{}
	replaceValue(reflect.ValueOf(job), replacements, seen)
	body, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("encode injected nomad job: %w", err)
	}
	if strings.Contains(string(body), "__VERSELF_") {
		return fmt.Errorf("nomad job %q contains unresolved __VERSELF_* site token", jobID(job))
	}
	return nil
}

func replaceValue(v reflect.Value, replacements map[string]string, seen map[uintptr]bool) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		ptr := v.Pointer()
		if ptr != 0 && seen[ptr] {
			return
		}
		if ptr != 0 {
			seen[ptr] = true
		}
		replaceValue(v.Elem(), replacements, seen)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		elem := v.Elem()
		if v.CanSet() {
			copyValue := reflect.New(elem.Type()).Elem()
			copyValue.Set(elem)
			replaceValue(copyValue, replacements, seen)
			v.Set(copyValue)
			return
		}
		replaceValue(elem, replacements, seen)
	case reflect.Struct:
		vtype := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if vtype.Field(i).PkgPath != "" {
				continue
			}
			field := v.Field(i)
			if field.CanSet() || field.Kind() == reflect.Pointer || field.Kind() == reflect.Slice || field.Kind() == reflect.Map || field.Kind() == reflect.Struct {
				replaceValue(field, replacements, seen)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			replaceValue(v.Index(i), replacements, seen)
		}
	case reflect.Map:
		if v.IsNil() || v.Type().Key().Kind() != reflect.String {
			return
		}
		for _, key := range v.MapKeys() {
			value := v.MapIndex(key)
			copyValue := reflect.New(value.Type()).Elem()
			copyValue.Set(value)
			replaceValue(copyValue, replacements, seen)
			v.SetMapIndex(key, copyValue)
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(replaceString(v.String(), replacements))
		}
	}
}

func replaceString(value string, replacements map[string]string) string {
	out := value
	for token, replacement := range replacements {
		out = strings.ReplaceAll(out, token, replacement)
	}
	return out
}

func jobID(job *api.Job) string {
	if job == nil || job.ID == nil {
		return ""
	}
	return *job.ID
}
