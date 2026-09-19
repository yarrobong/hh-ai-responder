package vacancyanalysis

import "testing"

func TestWorkModeClassificationDistinguishesAvailabilityAndRequirement(t *testing.T) {
	tests := []struct {
		name string
		text string
		want WorkModeClassification
	}{
		{name: "office available beside remote", text: "Если не любите удалёнку, есть офис в Москве", want: WorkModeOfficeAvailable},
		{name: "office only", text: "Работа только из офиса в Москве", want: WorkModeOfficeRequired},
		{name: "office mandatory", text: "Обязательна готовность к работе из офиса", want: WorkModeOfficeRequired},
		{name: "remote available", text: "Удалённая работа доступна", want: WorkModeRemoteAvailable},
		{name: "relocation mandatory", text: "Готовность к релокации в Москву обязательна", want: WorkModeRelocationRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyWorkMode(test.text); got != test.want {
				t.Fatalf("classification=%q, want %q", got, test.want)
			}
		})
	}
}

func TestOfficeAvailabilityIsNotALocationRequirement(t *testing.T) {
	if !workModeIsNonBlocking("Если не любите удалёнку, есть офис в Москве") {
		t.Fatal("office availability was treated as a hard location requirement")
	}
	if workModeIsNonBlocking("Работа только из офиса в Москве") {
		t.Fatal("office-only requirement was treated as non-blocking")
	}
}
