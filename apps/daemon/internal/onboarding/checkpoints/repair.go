package checkpoints

type RepairActionID string

const (
	RepairResumeStep           RepairActionID = "bootstrap.resume_step"
	RepairSkipStep             RepairActionID = "bootstrap.skip_step"
	RepairViewLogs             RepairActionID = "bootstrap.view_logs"
	RepairRunACFSDoctor        RepairActionID = "diagnostics.run_acfs_doctor"
	RepairReinstallDaemon      RepairActionID = "diagnostics.reinstall_daemon"
	RepairRefreshToolInventory RepairActionID = "diagnostics.refresh_tool_inventory"
	RepairVerifySkills         RepairActionID = "diagnostics.verify_skills"
	RepairRestartOracle        RepairActionID = "diagnostics.restart_oracle"
)

type RepairAction struct {
	ID                 RepairActionID `json:"id"`
	Label              string         `json:"label"`
	Description        string         `json:"description"`
	Kind               string         `json:"kind"`
	RequiresApproval   bool           `json:"requiresApproval"`
	CapabilityRequired string         `json:"capabilityRequired,omitempty"`
	AuditAction        string         `json:"auditAction"`
	AppliesToSteps     []string       `json:"appliesToSteps,omitempty"`
}

func RepairCatalog() []RepairAction {
	return []RepairAction{
		{
			ID:          RepairResumeStep,
			Label:       "Resume",
			Description: "Retry the failed wizard step from its last recorded checkpoint.",
			Kind:        "wizard.resume",
			AuditAction: "onboarding.repair.resume_step",
		},
		{
			ID:               RepairSkipStep,
			Label:            "Skip",
			Description:      "Mark the step skipped and continue in degraded mode.",
			Kind:             "wizard.skip",
			RequiresApproval: true,
			AuditAction:      "onboarding.repair.skip_step",
		},
		{
			ID:          RepairViewLogs,
			Label:       "View logs",
			Description: "Open the raw bootstrap log associated with the checkpoint.",
			Kind:        "diagnostics.view_logs",
			AuditAction: "onboarding.repair.view_logs",
		},
		{
			ID:                 RepairRunACFSDoctor,
			Label:              "Re-run ACFS doctor",
			Description:        "Run ACFS doctor in read-only mode unless the user approves fixes.",
			Kind:               "diagnostics.acfs_doctor",
			CapabilityRequired: "br.issues.read",
			AuditAction:        "onboarding.repair.run_acfs_doctor",
			AppliesToSteps:     []string{"acfs", "acfs-install", "tool-inventory"},
		},
		{
			ID:                 RepairReinstallDaemon,
			Label:              "Re-install daemon",
			Description:        "Reinstall the daemon through the verified bootstrap upgrade path.",
			Kind:               "diagnostics.daemon_reinstall",
			RequiresApproval:   true,
			CapabilityRequired: "git.status.read",
			AuditAction:        "onboarding.repair.reinstall_daemon",
			AppliesToSteps:     []string{"daemon", "daemon-install", "daemon-upgrade"},
		},
		{
			ID:                 RepairRefreshToolInventory,
			Label:              "Refresh tool inventory",
			Description:        "Reprobe tool versions and capability registry state.",
			Kind:               "diagnostics.tool_inventory_refresh",
			CapabilityRequired: "br.issues.read",
			AuditAction:        "onboarding.repair.refresh_tool_inventory",
			AppliesToSteps:     []string{"tool-inventory", "capabilities", "status-check"},
		},
		{
			ID:                 RepairVerifySkills,
			Label:              "Update / verify skills",
			Description:        "Verify jsm-pinned skills or use the jfp fallback path.",
			Kind:               "diagnostics.skills_verify",
			CapabilityRequired: "jsm.skill.verify",
			AuditAction:        "onboarding.repair.verify_skills",
			AppliesToSteps:     []string{"skills", "extensions"},
		},
		{
			ID:                 RepairRestartOracle,
			Label:              "Restart Oracle",
			Description:        "Restart the Oracle browser-mode bridge when configured.",
			Kind:               "diagnostics.oracle_restart",
			RequiresApproval:   true,
			CapabilityRequired: "oracle.serve.status",
			AuditAction:        "onboarding.repair.restart_oracle",
			AppliesToSteps:     []string{"oracle", "extensions"},
		},
	}
}

func RepairActionsForCheckpoint(checkpoint Checkpoint) []RepairAction {
	if checkpoint.Status != StatusFailed {
		return nil
	}
	catalog := RepairCatalog()
	out := make([]RepairAction, 0, 4)
	for _, action := range catalog {
		switch action.ID {
		case RepairResumeStep, RepairSkipStep, RepairViewLogs:
			out = append(out, cloneRepairAction(action))
		default:
			if actionMatchesStep(action, checkpoint.StepID) {
				out = append(out, cloneRepairAction(action))
			}
		}
	}
	return out
}

func actionMatchesStep(action RepairAction, stepID string) bool {
	if len(action.AppliesToSteps) == 0 {
		return false
	}
	for _, prefix := range action.AppliesToSteps {
		if stepID == prefix || len(stepID) > len(prefix) && stepID[:len(prefix)+1] == prefix+"." {
			return true
		}
	}
	return false
}

func cloneRepairAction(action RepairAction) RepairAction {
	action.AppliesToSteps = cloneStrings(action.AppliesToSteps)
	return action
}
