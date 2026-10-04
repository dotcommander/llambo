package costs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidPricesStayUnknown(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "N/A", "-1", "NaN", "+Inf", "-Inf", "1e999"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "prices.csv")
			data := "provider,model,input,output\np,model," + raw + "," + raw + "\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if price := got["p:model"]; price.InputExplicit || price.OutputExplicit {
				t.Fatalf("invalid price became explicit: %+v", price)
			}
		})
	}
	if value, explicit := parsePrice("0"); value != 0 || !explicit {
		t.Fatal("explicit zero rejected")
	}
}
