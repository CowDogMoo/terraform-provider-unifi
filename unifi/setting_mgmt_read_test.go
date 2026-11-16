package unifi

import (
	"context"
	"errors"
	"reflect"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// storedMgmtWithSSHKey is a mgmt setting carrying one SSH key whose date is
// in the given form: UniFi Network 11.0.99 returns a number, older releases a
// string.
func storedMgmtWithSSHKey(date any) map[string]any {
	return map[string]any{
		"_id":           "66ec38ab34dbcb19dce4a66c",
		"site_id":       "66ec388934dbcb19dce4a667",
		"key":           "mgmt",
		"x_ssh_enabled": true,
		"x_ssh_keys": []any{
			map[string]any{
				"name":        "laptop",
				"type":        "ssh-ed25519",
				"key":         "AAAAC3NzaC1lZDI1NTE5AAAAIExample",
				"comment":     "user@laptop",
				"fingerprint": "SHA256:example",
				"date":        date,
			},
		},
	}
}

func Test_mgmtFromRaw_sshKeyDate(t *testing.T) {
	for _, tt := range []struct {
		name string
		date any
		want string
	}{
		{"number (Network 11)", float64(1792980365277), "1792980365277"},
		{"string", "1792980365277", "1792980365277"},
		{"empty string", "", ""},
		{"null", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := storedMgmtWithSSHKey(tt.date)
			got, err := mgmtFromRaw(raw)
			if err != nil {
				t.Fatalf("mgmtFromRaw: %v", err)
			}
			want := []settings.SettingMgmtSSHKeys{{
				Name:        "laptop",
				KeyType:     "ssh-ed25519",
				Key:         "AAAAC3NzaC1lZDI1NTE5AAAAIExample",
				Comment:     "user@laptop",
				Fingerprint: "SHA256:example",
				Date:        tt.want,
			}}
			if !reflect.DeepEqual(got.SSHKeys, want) {
				t.Errorf("SSHKeys = %+v, want %+v", got.SSHKeys, want)
			}
			if !got.SSHEnabled || got.Key != "mgmt" || got.ID != "66ec38ab34dbcb19dce4a66c" {
				t.Errorf("other fields lost: %+v", got)
			}
			if !reflect.DeepEqual(raw, storedMgmtWithSSHKey(tt.date)) {
				t.Errorf("mgmtFromRaw modified its input: %v", raw)
			}
		})
	}
}

func Test_mgmtFromRaw_withoutSSHKeys(t *testing.T) {
	got, err := mgmtFromRaw(storedMgmt())
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoUpgrade || got.AutoUpgradeHour == nil || *got.AutoUpgradeHour != 3 ||
		len(got.SSHKeys) != 0 {
		t.Errorf("mgmtFromRaw(storedMgmt()) = %+v", got)
	}
}

func Test_readMgmtSetting(t *testing.T) {
	ctx := context.Background()

	for _, date := range []any{float64(1792980365277), "1792980365277"} {
		r, _ := newSettingsFakeController(t, []map[string]any{storedMgmtWithSSHKey(date)})
		got, err := r.readMgmtSetting(ctx, "default")
		if err != nil {
			t.Fatalf("date %#v: readMgmtSetting: %v", date, err)
		}
		if len(got.SSHKeys) != 1 || got.SSHKeys[0].Date != "1792980365277" {
			t.Errorf("date %#v: SSHKeys = %+v", date, got.SSHKeys)
		}
	}

	r, _ := newSettingsFakeController(t, []map[string]any{{"key": "country", "code": "276"}})
	_, err := r.readMgmtSetting(ctx, "default")
	var notFound *ui.NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("missing mgmt setting: err = %v, want *ui.NotFoundError", err)
	}

	if _, err := r.readMgmtSetting(ctx, "other-site"); err == nil {
		t.Error("unreadable settings: want error")
	}
}

// A mgmt write that leaves the SSH keys alone must not re-send them: the
// stored numeric date and the string go-unifi marshals are the same value.
func Test_writeSetting_mgmtNumericSSHKeyDateUnchanged(t *testing.T) {
	ctx := context.Background()
	r, puts := newSettingsFakeController(t, []map[string]any{
		storedMgmtWithSSHKey(float64(1792980365277)),
	})

	current, err := r.readMgmtSetting(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	current.WifimanEnabled = true
	if err := r.writeSetting(ctx, "default", current); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{"key": "mgmt", "wifiman_enabled": true}
	if got := puts()["mgmt"]; !reflect.DeepEqual(got, want) {
		t.Errorf("mgmt PUT = %v, want %v", got, want)
	}
}

// The reported failure: UniFi Network 11.0.99 returns x_ssh_keys[].date as a
// number, and every resource path that reads the mgmt setting (import,
// refresh, create, update) stopped with "Error Reading Mgmt Setting".
func Test_settingResource_mgmtNumericSSHKeyDate(t *testing.T) {
	ctx := context.Background()
	stored := func() []map[string]any {
		return []map[string]any{storedMgmtWithSSHKey(float64(1792980365277))}
	}

	t.Run("import and refresh", func(t *testing.T) {
		r, puts := newSettingsFakeController(t, stored())
		imported := importSettings(ctx, t, r, false)
		if imported.Diagnostics.HasError() {
			t.Fatalf("ImportState: %v", imported.Diagnostics)
		}
		var state settingResourceModel
		if d := imported.State.Get(ctx, &state); d.HasError() {
			t.Fatalf("reading imported state: %v", d)
		}
		var mgmt settingMgmtModel
		if d := state.Mgmt.As(ctx, &mgmt, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("mgmt: %v", d)
		}
		var ssh settingMgmtSSHModel
		if d := mgmt.SSH.As(ctx, &ssh, basetypes.ObjectAsOptions{}); d.HasError() {
			t.Fatalf("mgmt.ssh: %v", d)
		}
		var keys []sshKeyModel
		if d := ssh.Keys.ElementsAs(ctx, &keys, false); d.HasError() {
			t.Fatalf("mgmt.ssh.keys: %v", d)
		}
		if len(keys) != 1 || keys[0].Name.ValueString() != "laptop" ||
			keys[0].Key.ValueString() != "AAAAC3NzaC1lZDI1NTE5AAAAIExample" {
			t.Errorf("imported mgmt.ssh.keys = %+v, want the controller's one key", keys)
		}

		read := &fwresource.ReadResponse{State: imported.State, Identity: imported.Identity}
		r.Read(ctx, fwresource.ReadRequest{
			State: imported.State, Identity: imported.Identity,
		}, read)
		if read.Diagnostics.HasError() {
			t.Fatalf("Read: %v", read.Diagnostics)
		}
		if diffs, err := imported.State.Raw.Diff(read.State.Raw); err != nil || len(diffs) != 0 {
			t.Errorf("refresh after import changed the state: %v (%v)", diffs, err)
		}
		if got := puts(); len(got) != 0 {
			t.Errorf("import and refresh wrote %v, want no writes", got)
		}
	})

	// mgmt.ssh.keys is in neither the configuration nor the state, so each
	// write carries the stored keys through and does not re-send them.
	t.Run("create and update", func(t *testing.T) {
		r, puts := newSettingsFakeController(t, stored())
		mgmtConfig := func(wifiman bool) map[string]any {
			return map[string]any{"mgmt": map[string]any{"wifiman_enabled": wifiman}}
		}

		created := createSettings(t, r, mgmtConfig(true))
		if created.Diagnostics.HasError() {
			t.Fatalf("Create: %v", created.Diagnostics)
		}
		want := map[string]map[string]any{"mgmt": {"key": "mgmt", "wifiman_enabled": true}}
		if got := puts(); !reflect.DeepEqual(got, want) {
			t.Errorf("create wrote %v, want %v", got, want)
		}

		s := resourceSchema(t, r)
		attrs := mgmtConfig(false)
		updated := &fwresource.UpdateResponse{State: created.State, Identity: created.Identity}
		r.Update(ctx, fwresource.UpdateRequest{
			Plan: tfPlan(t, s, attrs), Config: tfConfig(t, s, attrs), State: created.State,
		}, updated)
		if updated.Diagnostics.HasError() {
			t.Fatalf("Update: %v", updated.Diagnostics)
		}
		want = map[string]map[string]any{"mgmt": {"key": "mgmt", "wifiman_enabled": false}}
		if got := puts(); !reflect.DeepEqual(got, want) {
			t.Errorf("update wrote %v, want %v", got, want)
		}
	})
}
