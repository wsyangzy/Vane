package routing

import "time"

// SessionAffinity contains only a hashed lookup key and configuration identity.
// Classification results and in-progress turns are never restored after restart.
type SessionAffinity struct {
	Provider     string    `json:"provider"`
	ProfileID    int64     `json:"profile_id"`
	Key          string    `json:"key"`
	Candidate    Candidate `json:"candidate"`
	Model        string    `json:"model"`
	ExpiresAt    time.Time `json:"expires_at"`
	SuccessfulAt time.Time `json:"successful_at"`
}

func (r *ProfileRuntime) ExportSessionAffinity() []SessionAffinity {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	result := []SessionAffinity{}
	for key, entry := range r.affinity {
		if entry.session && entry.failures == 0 && now.Before(entry.expires) {
			result = append(result, SessionAffinity{Provider: entry.candidate.Provider, ProfileID: entry.profileID, Key: key, Candidate: entry.candidate, Model: entry.model, ExpiresAt: entry.expires, SuccessfulAt: entry.success})
		}
	}
	return result
}

func (r *ProfileRuntime) SeedSessionAffinity(entries []SessionAffinity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for _, entry := range entries {
		if entry.Provider != r.config.Provider || len(entry.Key) != 64 || !now.Before(entry.ExpiresAt) || entry.ExpiresAt.After(now.Add(time.Hour)) {
			continue
		}
		profile, ok := runtimeProfile(r.config, entry.ProfileID)
		if !ok || profile.AffinityMode == AffinityNone {
			continue
		}
		// Validate every restored identity against today's resources. A new
		// in-process state version may be assigned after a normal restart.
		for _, resource := range r.config.Candidates {
			prepare := r.config.PrepareCandidate
			if prepare == nil {
				prepare = CopyCandidateForModel
			}
			resource = prepare(resource, entry.Model, entry.Candidate.Model)
			candidate := resource.Identity()
			prior := entry.Candidate
			candidate.StateVersion = 0
			prior.StateVersion = 0
			if resource.Enabled && candidate == prior {
				r.affinity[entry.Key] = profileAffinity{candidate: resource.Identity(), model: entry.Model, expires: entry.ExpiresAt, success: entry.SuccessfulAt, used: now, session: true, profileID: entry.ProfileID}
				break
			}
		}
	}
	r.trimAffinityLocked(now)
}
