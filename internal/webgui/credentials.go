package webgui

import (
	"errors"
	"fmt"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/routespec"
)

func (a *App) storeFingerprint(expected config.Host, hop int, fingerprint string, generation, version uint64) error {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store == nil || a.locking || a.generation != generation || a.configVersion != version {
		return errors.New("确认指纹期间会话或配置已改变")
	}
	next := a.document.Clone()
	host := next.HostByID(expected.ID)
	if host == nil || host.Disabled || host.RouteSpec != expected.RouteSpec || host.Address != expected.Address || host.Port != expected.Port {
		return errors.New("确认指纹对应的主机配置已改变")
	}
	if hop < 0 {
		if host.HostFingerprint != "" && host.HostFingerprint != fingerprint {
			return errors.New("主机指纹与已确认的指纹冲突；未覆盖")
		}
		host.HostFingerprint = fingerprint
	} else {
		for len(host.HopFingerprints) <= hop {
			host.HopFingerprints = append(host.HopFingerprints, "")
		}
		if host.HopFingerprints[hop] != "" && host.HopFingerprints[hop] != fingerprint {
			return errors.New("跳点指纹与已确认的指纹冲突；未覆盖")
		}
		host.HopFingerprints[hop] = fingerprint
	}
	if err := a.store.Save(a.password, next); err != nil {
		return err
	}
	a.document = next
	return nil
}

// Commit prompted credentials only after the entire route authenticates. The
// owner recorded before composing relay and target routes controls every write.
func (a *App) acceptConnectionCredentials(targetID string, candidate routeCandidate, answers map[int]challengeAnswer, generation, version uint64) error {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store == nil || a.locking || a.generation != generation || a.configVersion != version {
		return errors.New("连接期间配置或会话已改变；未缓存或保存认证信息")
	}
	next := a.document.Clone()
	durable := false
	for index, answer := range answers {
		owner := candidate.owners[index]
		host := next.HostByID(owner.host.ID)
		if host == nil || host.Disabled {
			return errors.New("认证信息对应的原始会话已删除或禁用")
		}
		if !answer.Save {
			continue
		}
		if host.RouteSpec != "" {
			spec, err := routespec.WithPasswords(host.RouteSpec, map[int]string{owner.hop: answer.Value})
			if err != nil {
				return err
			}
			host.RouteSpec, host.HopPasswords = spec, nil
		} else {
			host.Password = answer.Value
		}
		durable = true
	}
	if durable {
		if err := a.store.Save(a.password, next); err != nil {
			return fmt.Errorf("SSH 认证成功，但保存密码失败；配置未改变: %w", err)
		}
		a.document = next
	}
	for index, answer := range answers {
		owner := candidate.owners[index]
		if a.runtimePasswords[owner.host.ID] == nil {
			a.runtimePasswords[owner.host.ID] = make(map[int]string)
		}
		a.runtimePasswords[owner.host.ID][owner.hop] = answer.Value
	}
	if !candidate.temporaryProxy {
		a.sessionSSH[targetID] = true
	}
	return nil
}
