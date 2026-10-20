package api

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/app"
)

// Moving App out of main must preserve the UI API without exposing host-only
// lifecycle operations. Inspect the actual service registrations used by Wails.
func TestRegisteredServicesPreserveApplicationAPI(t *testing.T) {
	hostOnly := map[string]bool{
		"AttachDesktop": true,
		"Startup":       true,
		"Shutdown":      true,
		"WindowReady":   true,
		"IsQuitting":    true,
		"MarkQuitting":  true,
	}
	appType := reflect.TypeOf((*app.App)(nil))
	remaining := make(map[string]reflect.Method)
	for i := 0; i < appType.NumMethod(); i++ {
		method := appType.Method(i)
		if !hostOnly[method.Name] {
			remaining[method.Name] = method
		}
	}

	seen := make(map[string]string)
	for _, service := range Services(nil) {
		serviceType := reflect.TypeOf(service.Instance())
		for i := 0; i < serviceType.NumMethod(); i++ {
			method := serviceType.Method(i)
			if hostOnly[method.Name] {
				t.Errorf("host-only method exposed by %s: %s", serviceType, method.Name)
				continue
			}
			if previous, exists := seen[method.Name]; exists {
				t.Errorf("ambiguous bridge method %s: %s and %s", method.Name, previous, serviceType)
			}
			seen[method.Name] = serviceType.String()
			original, exists := appType.MethodByName(method.Name)
			if !exists {
				t.Errorf("%s.%s has no application method", serviceType, method.Name)
				continue
			}
			if !sameSignature(original.Type, method.Type) {
				t.Errorf("%s.%s signature changed: %s, want %s", serviceType, method.Name, method.Type, original.Type)
			}
			delete(remaining, method.Name)
		}
	}
	for name := range remaining {
		t.Errorf("application method missing from Wails services: %s", name)
	}
}

func sameSignature(a, b reflect.Type) bool {
	if a.NumIn() != b.NumIn() || a.NumOut() != b.NumOut() || a.IsVariadic() != b.IsVariadic() {
		return false
	}
	// The receiver moves from *app.App to a service; arguments and results do not.
	for i := 1; i < a.NumIn(); i++ {
		if a.In(i) != b.In(i) {
			return false
		}
	}
	for i := 0; i < a.NumOut(); i++ {
		if a.Out(i) != b.Out(i) {
			return false
		}
	}
	return true
}

func TestGeneratedBindingsMatchRegisteredServices(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository from test source")
	}
	bindingsRoot := filepath.Join(filepath.Dir(source), "../../../frontend/src/bindings")
	callName := regexp.MustCompile(`\$Call\.ByName\("([^"]+)"`)
	for _, service := range Services(nil) {
		serviceType := reflect.TypeOf(service.Instance()).Elem()
		bindingPath := filepath.Join(bindingsRoot, filepath.FromSlash(serviceType.PkgPath()), strings.ToLower(serviceType.Name())+".js")
		data, err := os.ReadFile(bindingPath)
		if err != nil {
			t.Errorf("read generated binding for %s: %v", serviceType, err)
			continue
		}
		calls := make(map[string]bool)
		for _, match := range callName.FindAllStringSubmatch(string(data), -1) {
			calls[match[1]] = true
		}
		pointerType := reflect.PointerTo(serviceType)
		for i := 0; i < pointerType.NumMethod(); i++ {
			method := pointerType.Method(i)
			qualifiedName := serviceType.PkgPath() + "." + serviceType.Name() + "." + method.Name
			if !calls[qualifiedName] {
				t.Errorf("generated binding missing registered method: %s", qualifiedName)
			}
			delete(calls, qualifiedName)
		}
		for name := range calls {
			t.Errorf("generated binding references an unregistered method: %s", name)
		}
	}
}
