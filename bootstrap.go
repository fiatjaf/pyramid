package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"

	"fiatjaf.com/nostr"
)

// applyPlatformEnvOverrides enforces operator-managed settings on every boot
// (unlike bootstrapFromEnv, which only runs before first-run setup).
//
//   - The domain is host-bound: it must match where the relay is actually served
//     or the NIP-98 login cookie's domain tag won't match Settings.Domain and
//     nobody can authenticate. A restore can leave settings.json carrying a
//     different relay's domain (backups clone settings wholesale), so we always
//     re-assert PYRAMID_DOMAIN here — otherwise a cross-relay restore silently
//     breaks the admin UI.
//   - Filter cost ~= authors per filter, so relays with large auto-allowed member
//     sets need far more per-IP headroom than the upstream default (7200).
//
// A no-op for anyone not setting these env vars (e.g. standalone installs).
func applyPlatformEnvOverrides() {
	if cost, err := strconv.Atoi(os.Getenv("PYRAMID_MAX_TOTAL_COST_OPEN")); err == nil && cost > 0 {
		global.Settings.Limits.MaxTotalCostOpen = cost
	}

	if domainInput := os.Getenv("PYRAMID_DOMAIN"); domainInput != "" {
		if domain, err := normalizeDomainInput(domainInput); err == nil && domain != "" &&
			domain != global.Settings.Domain {
			log.Info().Str("was", global.Settings.Domain).Str("now", domain).
				Msg("re-asserting host domain from PYRAMID_DOMAIN")
			global.Settings.Domain = domain
			if err := global.SaveUserSettings(); err != nil {
				log.Warn().Err(err).Msg("could not persist re-asserted domain")
			}
		}
	}
}

// bootstrapFromEnv performs the one-time setup normally done through the
// /setup/domain and /setup/root web pages, driven by environment variables
// instead. This allows non-interactive provisioning (containers, orchestrators).
//
// It only acts when PYRAMID_ROOT_PUBKEY is set and the relay has no root users
// yet, so it is a no-op for already-configured relays and for anyone not using
// the env vars.
func bootstrapFromEnv() error {
	rootInput := os.Getenv("PYRAMID_ROOT_PUBKEY")
	if rootInput == "" {
		return nil
	}
	if pyramid.HasRootUsers() {
		return nil
	}

	rootPK := global.PubKeyFromInput(rootInput)
	if rootPK == nostr.ZeroPK {
		return fmt.Errorf("PYRAMID_ROOT_PUBKEY is not a valid public key: %q", rootInput)
	}

	if global.Settings.Domain == "" {
		if domainInput := os.Getenv("PYRAMID_DOMAIN"); domainInput != "" {
			domain, err := normalizeDomainInput(domainInput)
			if err != nil {
				return fmt.Errorf("PYRAMID_DOMAIN is invalid: %w", err)
			}
			global.Settings.Domain = domain
		}
	}

	if name := os.Getenv("PYRAMID_RELAY_NAME"); name != "" {
		global.Settings.RelayName = name
	}
	if desc := os.Getenv("PYRAMID_RELAY_DESCRIPTION"); desc != "" {
		global.Settings.RelayDescription = desc
	}
	if v, err := strconv.ParseBool(os.Getenv("PYRAMID_GROUPS_ENABLED")); err == nil {
		global.Settings.Groups.Enabled = v
	}
	if v, err := strconv.ParseBool(os.Getenv("PYRAMID_BLOSSOM_ENABLED")); err == nil {
		global.Settings.Blossom.Enabled = v
	}
	if mb, err := strconv.Atoi(os.Getenv("PYRAMID_BLOSSOM_MAX_UPLOAD_MB")); err == nil && mb > 0 {
		global.Settings.Blossom.MaxUserUploadSize = mb
	}

	if err := global.SaveUserSettings(); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	if err := pyramid.AddAction(pyramid.ActionInvite, pyramid.AbsoluteKey, rootPK); err != nil {
		return fmt.Errorf("failed to add root user: %w", err)
	}

	log.Info().Str("root", rootPK.Hex()).Str("domain", global.Settings.Domain).
		Msg("bootstrapped relay from environment")

	if serviceInput := os.Getenv("PYRAMID_SERVICE_PUBKEY"); serviceInput != "" {
		servicePK := global.PubKeyFromInput(serviceInput)
		if servicePK == nostr.ZeroPK {
			return fmt.Errorf("PYRAMID_SERVICE_PUBKEY is not a valid public key: %q", serviceInput)
		}
		if servicePK != rootPK {
			if err := pyramid.AddAction(pyramid.ActionInvite, pyramid.AbsoluteKey, servicePK); err != nil {
				return fmt.Errorf("failed to add service user: %w", err)
			}
			log.Info().Str("service", servicePK.Hex()).Msg("added service pubkey as root member")
		}
	}

	return nil
}
