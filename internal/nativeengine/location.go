package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeLocationAssign uint64 = 20_200 + iota
	nativeLocationReplace
	nativeLocationReload
	nativeLocationToString
)

func (realm *Realm) newLocation(context *browserruntime.TaskContext, document browser.DocumentGeneration) (memory.Ref, error) {
	location, err := realm.newHostWrapperLocked(context, memory.HostObject{
		Class: hostClassLocation, Scope: uint64(document), Identity: 1,
	}, realm.active.ObjectPrototype)
	if err != nil {
		return memory.Ref{}, err
	}
	for _, method := range []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"assign", 1, nativeLocationAssign},
		{"replace", 1, nativeLocationReplace},
		{"reload", 0, nativeLocationReload},
		{"toString", 0, nativeLocationToString},
	} {
		callable, methodErr := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if methodErr != nil {
			return memory.Ref{}, methodErr
		}
		if methodErr := defineData(context, location, method.name, memory.RefValue(callable), true, false, true); methodErr != nil {
			return memory.Ref{}, methodErr
		}
	}
	return location, nil
}

func (realm *Realm) locationAssign(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.navigateLocation(context, this, arguments, browser.LocationAssign)
}

func (realm *Realm) locationReplace(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.navigateLocation(context, this, arguments, browser.LocationReplace)
}

func (realm *Realm) locationReload(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.navigateLocation(context, this, arguments, browser.LocationReload)
}

func (realm *Realm) navigateLocation(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value, action browser.LocationNavigationAction) (memory.Value, error) {
	if err := realm.requireLocation(context, this); err != nil {
		return memory.Value{}, err
	}
	rawURL := ""
	if action != browser.LocationReload {
		var err error
		rawURL, err = stringArgument(context, arguments, 0)
		if err != nil {
			return memory.Value{}, err
		}
	}
	host, ok := realm.host.(browser.SessionHistoryHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose location navigation")
	}
	if err := host.NavigateLocation(rawURL, action); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) locationToString(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if err := realm.requireLocation(context, this); err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.SessionHistoryHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose location")
	}
	value, err := host.LocationComponent(browser.LocationHref)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) requireLocation(context *browserruntime.TaskContext, value memory.Value) error {
	if !value.IsRef() {
		return fmt.Errorf("%w: incompatible Location receiver", browserruntime.ErrOperandType)
	}
	record, found, err := realm.facadeRecord(context, value.Ref())
	if err != nil {
		return err
	}
	if !found || record.Class != hostClassLocation {
		return fmt.Errorf("%w: incompatible Location receiver", browserruntime.ErrOperandType)
	}
	return nil
}

func locationComponent(name string) (browser.LocationComponent, bool) {
	switch name {
	case "href":
		return browser.LocationHref, true
	case "origin":
		return browser.LocationOrigin, true
	case "protocol":
		return browser.LocationProtocol, true
	case "host":
		return browser.LocationHost, true
	case "hostname":
		return browser.LocationHostname, true
	case "port":
		return browser.LocationPort, true
	case "pathname":
		return browser.LocationPathname, true
	case "search":
		return browser.LocationSearch, true
	case "hash":
		return browser.LocationHash, true
	default:
		return 0, false
	}
}
