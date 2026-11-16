package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// mgmtSettingKey is the setting key of the site management settings.
const mgmtSettingKey = "mgmt"

// readMgmtSetting fetches the site's mgmt setting and decodes it into the
// go-unifi struct. It returns *ui.NotFoundError when the controller has never
// stored the setting, matching ui.GetSetting.
//
// ui.GetSetting cannot be used here: UniFi Network 11 returns each
// x_ssh_keys[].date as a number (milliseconds since the epoch), older releases
// as a string, and go-unifi declares SettingMgmtSSHKeys.Date as a string, so
// the typed decode fails with "cannot unmarshal number into Go struct field
// .Alias.date of type string". The raw setting is decoded instead, with
// numeric dates rewritten to their decimal string form first.
func (r *settingResource) readMgmtSetting(
	ctx context.Context,
	site string,
) (*settings.Mgmt, error) {
	data, found, err := r.readRawSettingData(ctx, site, mgmtSettingKey)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &ui.NotFoundError{}
	}
	return mgmtFromRaw(data)
}

// mgmtFromRaw decodes a raw mgmt setting into the go-unifi struct, accepting
// x_ssh_keys[].date as either a string or a number. data is not modified.
func mgmtFromRaw(data map[string]any) (*settings.Mgmt, error) {
	normalized := data
	if keys, ok := data["x_ssh_keys"].([]any); ok {
		normalized = maps.Clone(data)
		normalizedKeys := make([]any, len(keys))
		for i, key := range keys {
			normalizedKeys[i] = sshKeyWithStringDate(key)
		}
		normalized["x_ssh_keys"] = normalizedKeys
	}

	buf, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encoding mgmt setting: %w", err)
	}
	mgmt := &settings.Mgmt{}
	if err := json.Unmarshal(buf, mgmt); err != nil {
		return nil, fmt.Errorf("decoding mgmt setting: %w", err)
	}
	return mgmt, nil
}

// sshKeyWithStringDate returns a copy of a raw x_ssh_keys entry whose numeric
// date is replaced by its exact decimal string, e.g. 1792980365277 becomes
// "1792980365277". Entries without a numeric date are returned unchanged.
func sshKeyWithStringDate(key any) any {
	m, ok := key.(map[string]any)
	if !ok {
		return key
	}
	date, ok := m["date"].(float64)
	if !ok {
		return key
	}
	out := maps.Clone(m)
	out["date"] = strconv.FormatFloat(date, 'f', -1, 64)
	return out
}
