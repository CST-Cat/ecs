package i18n_test

import (
	"testing"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

func TestModuleDescriptorTitleIsTranslated(t *testing.T) {
	descriptor, ok := config.ModuleDescriptorFor("system")
	if !ok || descriptor.TitleKey == "" {
		t.Fatalf("system descriptor = %+v, ok=%v", descriptor, ok)
	}
	for _, lang := range i18n.Supported() {
		if !i18n.Has(lang, descriptor.TitleKey) {
			t.Errorf("system title %q missing %s translation", descriptor.TitleKey, lang)
		}
	}
}
