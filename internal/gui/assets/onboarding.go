package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

var (
	//go:embed onboarding/Front_panel.png
	onboardingStep01 []byte
	//go:embed onboarding/Front_panel1.png
	onboardingStep02 []byte
	//go:embed onboarding/Front_panel2.png
	onboardingStep03 []byte
)

var (
	OnboardingStep01 = fyne.NewStaticResource("Front_panel.png", onboardingStep01)
	OnboardingStep02 = fyne.NewStaticResource("Front_panel1.png", onboardingStep02)
	OnboardingStep03 = fyne.NewStaticResource("Front_panel2.png", onboardingStep03)
)
