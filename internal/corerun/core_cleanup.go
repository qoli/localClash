package corerun

import "localclash/internal/coredownload"

// PruneUnusedCores defers cleanup while another flavor is still running. Saving
// a desired selection alone does not mean that its runtime has been activated.
// Callers must first validate the selected binary against the selected config.
func PruneUnusedCores(opts StatusOptions) ([]string, error) {
	for _, process := range findManagedRuntimeProcesses() {
		if _, err := InspectManagedRuntimeProcess(process.PID, opts.CorePath, opts.ConfigPath, opts.WorkDir); err != nil {
			return nil, nil
		}
	}
	return coredownload.PruneUnused(opts.CorePath)
}
