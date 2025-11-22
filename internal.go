package serial

import (
	"errors"
	"fmt"
	"github.com/Station-Manager/types"
)

// handleOpenError closes the port and joins any error from closing with the original error
// This method assumes the mutex is already held by the caller
func (p *Service) handleOpenError(err error) error {
	e := p.closeWithoutLock()
	if e != nil {
		err = errors.Join(err, e)
	}
	return err
}

// closeWithoutLock performs close operations without acquiring the mutex
// This method assumes the mutex is already held by the caller
func (p *Service) closeWithoutLock() error {
	h := p.handle
	p.handle = nil
	p.isOpen.Store(false)
	if h != nil {
		return h.Close()
	}
	return nil
}

func (p *Service) serialPortConfig() (*types.SerialConfig, error) {
	required := p.ConfigService.RequiredConfigs()
	//	rigConfig, err := p.ConfigService.RigConfig(required.RigName)
	rigConfig, err := p.ConfigService.RigConfigByID(required.RigID)
	if err != nil {
		return nil, fmt.Errorf("serialPortConfig: config not found for rig '%d': %v", required.RigID, err)
	}
	return &rigConfig.SerialConfig, nil
}
