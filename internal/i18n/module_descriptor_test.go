package i18n_test

import (
	"testing"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/probe"
)

func TestModuleDescriptorsHaveLocalizedMetadata(t *testing.T) {
	descriptors := config.ModuleDescriptors(probe.BuiltinCatalog())
	if len(descriptors) == 0 {
		t.Fatal("module descriptor list is empty")
	}
	for _, descriptor := range descriptors {
		for _, lang := range i18n.Supported() {
			if !i18n.Has(lang, descriptor.TitleKey) || !i18n.Has(lang, descriptor.DescriptionKey) {
				t.Errorf("%s metadata missing %s translation: title=%q description=%q", descriptor.ID, lang, descriptor.TitleKey, descriptor.DescriptionKey)
			}
		}
		if descriptor.WizardGroup == "" && descriptor.WizardQuestionKey != "" {
			t.Errorf("%s has question without wizard group", descriptor.ID)
		}
		if descriptor.WizardGroup != "" && descriptor.WizardQuestionKey == "" {
			t.Errorf("%s has wizard group without question", descriptor.ID)
		}
		if descriptor.WizardQuestionKey != "" {
			for _, lang := range i18n.Supported() {
				if !i18n.Has(lang, descriptor.WizardQuestionKey) {
					t.Errorf("%s wizard question %q missing %s translation", descriptor.ID, descriptor.WizardQuestionKey, lang)
				}
			}
		}
	}
}
