// SPDX-License-Identifier: AGPL-3.0-only
package system

import "testing"

func TestParseOSRelease(t *testing.T) {
	content := "NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nID=ubuntu\n# comment\nBADLINE\n"
	kv := parseOSRelease(content)
	if kv["ID"] != "ubuntu" {
		t.Errorf("ID = %q", kv["ID"])
	}
	if kv["VERSION_ID"] != "24.04" {
		t.Errorf("VERSION_ID = %q", kv["VERSION_ID"])
	}
	if kv["PRETTY_NAME"] != "Ubuntu 24.04.1 LTS" {
		t.Errorf("PRETTY_NAME = %q", kv["PRETTY_NAME"])
	}
	if _, ok := kv["BADLINE"]; ok {
		t.Error("bad line should be skipped")
	}
}

func TestMachineArch(t *testing.T) {
	if machineArch() == "" {
		t.Error("empty architecture")
	}
}
