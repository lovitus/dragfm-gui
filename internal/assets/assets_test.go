package assets

import (
	"bytes"
	"debug/elf"
	"testing"
)

func TestEmbeddedLinuxAgents(t *testing.T) {
	t.Parallel()
	for architecture, machine := range map[string]elf.Machine{
		"amd64": elf.EM_X86_64,
		"arm64": elf.EM_AARCH64,
		"386":   elf.EM_386,
		"armv7": elf.EM_ARM,
	} {
		payload, hash, err := LinuxAgent(architecture)
		if err != nil {
			t.Fatalf("%s: %v", architecture, err)
		}
		if len(payload) == 0 || len(hash) != 64 {
			t.Fatalf("%s: payload=%d hash=%q", architecture, len(payload), hash)
		}
		binary, err := elf.NewFile(bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("%s: %v", architecture, err)
		}
		assertStaticLinuxELF(t, architecture, binary, machine)
		_ = binary.Close()
	}
}

func TestEmbeddedHans(t *testing.T) {
	type officialRelease struct {
		machine elf.Machine
		sha256  string
	}
	for architecture, release := range map[string]officialRelease{
		"amd64": {elf.EM_X86_64, "e486957d351852fc559b0f4be9f20c57c9eaa32ecd7395793a8858ce65e14958"},
		"arm64": {elf.EM_AARCH64, "342700230a9f9e4448ebc87e927e025f60676e7c20c980e5a2ea800cb79dce47"},
		"386":   {elf.EM_386, "c8eadadcb14f27cb6551d32beb9aeae1ddd965cd22702862e5ec70de9a4d67e0"},
		"armv7": {elf.EM_ARM, "c43f758e2dfb6960f8ce7554abeb55c4b9f4e48d5cf2f949fee5e593452e49aa"},
	} {
		payload, hash, err := LinuxHans(architecture)
		if err != nil {
			t.Fatalf("%s: %v", architecture, err)
		}
		if len(payload) == 0 || hash != release.sha256 {
			t.Fatalf("%s: payload=%d hash=%q", architecture, len(payload), hash)
		}
		binary, err := elf.NewFile(bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("%s: %v", architecture, err)
		}
		assertStaticLinuxELF(t, architecture, binary, release.machine)
		_ = binary.Close()
	}
}

func assertStaticLinuxELF(t *testing.T, architecture string, binary *elf.File, machine elf.Machine) {
	t.Helper()
	if binary.FileHeader.OSABI != elf.ELFOSABI_NONE && binary.FileHeader.OSABI != elf.ELFOSABI_LINUX {
		t.Fatalf("%s: unexpected ELF OS ABI %s", architecture, binary.FileHeader.OSABI)
	}
	if binary.FileHeader.Machine != machine {
		t.Fatalf("%s: ELF machine %s, want %s", architecture, binary.FileHeader.Machine, machine)
	}
	for _, program := range binary.Progs {
		if program.Type == elf.PT_INTERP {
			t.Fatalf("%s: helper must be statically linked; embedded payload has PT_INTERP", architecture)
		}
	}
}
