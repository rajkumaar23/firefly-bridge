package institution

import (
	"fmt"
	"testing"

	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"gopkg.in/yaml.v3"
)

const institutionYAML = `
name: Test Bank
login:
  - type: navigate
    url: "https://bank.example.com/login"
%s
accounts:
  - name: Checking
    firefly_account_id: 1
    account_type: regular
    balance:
      - type: balance
        selector: "#bal"
    transactions:
      - type: transactions
        csv:
          fields:
            date:
              column: 1
              format: "2006-01-02"
            description:
              column: 2
            amount:
              column: 3
`

func TestLogoutFlowUnmarshals(t *testing.T) {
	const logoutBlock = `logout:
  - type: click
    selector: "#account-menu"
  - type: wait_not_visible
    selector: "#login-form"
`

	var inst Institution
	if err := yaml.Unmarshal([]byte(fmt.Sprintf(institutionYAML, logoutBlock)), &inst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(inst.LogoutFlow) != 2 {
		t.Fatalf("LogoutFlow len = %d, want 2", len(inst.LogoutFlow))
	}
	if got := inst.LogoutFlow[0].Step.Type(); got != chromedp.StepClick {
		t.Errorf("logout step 0 type = %s, want %s", got, chromedp.StepClick)
	}
	if got := inst.LogoutFlow[1].Step.Type(); got != chromedp.StepWaitNotVisible {
		t.Errorf("logout step 1 type = %s, want %s", got, chromedp.StepWaitNotVisible)
	}
}

func TestNoLogoutFlowIsEmpty(t *testing.T) {
	var inst Institution
	if err := yaml.Unmarshal([]byte(fmt.Sprintf(institutionYAML, "")), &inst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(inst.LogoutFlow) != 0 {
		t.Errorf("LogoutFlow len = %d, want 0", len(inst.LogoutFlow))
	}
}
