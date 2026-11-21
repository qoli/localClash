package poolcontract

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"localclash/internal/capability"
)

func TestBulkFillsFloorAndRetainsMeasuredMembers(t *testing.T) {
	for _, tc := range []struct{ nodes, measured, want int }{{40, 0, 30}, {40, 5, 30}, {60, 35, 35}, {12, 2, 12}, {1, 0, 1}, {0, 0, 0}} {
		var input Input
		var names []string
		result := capability.Result{BulkQuality: &capability.BulkQuality{}, BulkTiers: map[string]string{}}
		for i := 0; i < tc.nodes; i++ {
			name := fmt.Sprintf("exit %02d", i)
			names = append(names, name)
			input.Proxies = append(input.Proxies, map[string]any{"name": name, "type": "http"})
			if i < tc.measured {
				result.Qualified = append(result.Qualified, name)
				result.BulkTiers[name] = "4K-stable"
			}
		}
		FillBulk(&result, append(names, names...))
		if result.QualifiedCount != tc.want || result.BulkQuality.Reserves != tc.want-tc.measured {
			t.Fatalf("%+v: %+v", tc, result)
		}
		pool, _ := Lookup(BulkCapability)
		data, published, err := ResultProviderData(pool, input, result)
		if err != nil {
			t.Fatal(err)
		}
		if tc.want == 0 {
			if !reflect.DeepEqual(published, []string{RejectName}) {
				t.Fatal("zero candidates must reject")
			}
			continue
		}
		if len(published) != tc.want {
			t.Fatalf("membership %d", len(published))
		}
		for i, name := range published {
			tier := "reserve"
			if i < tc.measured {
				tier = "4K-stable"
			}
			if name != "YT/"+tier+" "+names[i] || input.Proxies[i]["name"] != names[i] || !strings.Contains(string(data), name) {
				t.Fatal("wrong rank, name, or input mutation", name)
			}
		}
	}
}
