package i18n_test

import (
	"testing"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

// Display keys are part of the core descriptor contract.  Keep this check in
// an external test package so i18n can remain dependency-free (config itself
// uses i18n for validation errors) while still catching a new module that
// forgot either language's title or description.
func TestModuleDescriptorDisplayKeysAreTranslated(t *testing.T) {
	for _, descriptor := range config.ModuleDescriptors() {
		for _, lang := range i18n.Supported() {
			if descriptor.TitleKey == "" || !i18n.Has(lang, descriptor.TitleKey) {
				t.Errorf("module %q missing %s title translation for key %q", descriptor.ID, lang, descriptor.TitleKey)
			}
			if descriptor.DescriptionKey == "" || !i18n.Has(lang, descriptor.DescriptionKey) {
				t.Errorf("module %q missing %s description translation for key %q", descriptor.ID, lang, descriptor.DescriptionKey)
			}
		}
	}
}
