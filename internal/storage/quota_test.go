package storage

import "testing"

// Literal byte counts for the storage quota assertions.
// Derived from the documented limits (100 MB free, 10 GB per seat), not by
// re-importing the StorageQuota* constants, so a regression in the constants
// themselves still fails the assertions.
const (
	quotaFreeInt = int64(104857600)   // 100 * 1024 * 1024 = 100 MB
	quotaProInt  = int64(10737418240) // 10 * 1024 * 1024 * 1024 = 10 GB
)

func TestUserStorageQuota(t *testing.T) {
	tests := []struct {
		name string
		tier UserTier
		want int64
	}{
		{name: "free tier", tier: UserTierFree, want: quotaFreeInt},
		{name: "pro tier", tier: UserTierPro, want: quotaProInt},
		// Zero value UserTier("") — a user row that predates the tier column
		// or was never explicitly set. Must fall back to the free quota.
		{name: "zero value tier", tier: UserTier(""), want: quotaFreeInt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Tier: tt.tier}
			if got := u.StorageQuota(); got != tt.want {
				t.Errorf("StorageQuota() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUserHasPro(t *testing.T) {
	tests := []struct {
		name string
		tier UserTier
		want bool
	}{
		{name: "pro tier", tier: UserTierPro, want: true},
		{name: "free tier", tier: UserTierFree, want: false},
		{name: "zero value tier", tier: UserTier(""), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Tier: tt.tier}
			if got := u.HasPro(); got != tt.want {
				t.Errorf("HasPro() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserIsOverQuota(t *testing.T) {
	tests := []struct {
		name string
		tier UserTier
		used int64
		want bool
	}{
		// Free tier (quota = 100 MB).
		{name: "free: one byte under quota", tier: UserTierFree, used: quotaFreeInt - 1, want: false},
		// Boundary: exactly AT the quota counts as over (>=, not >).
		{name: "free: exactly at quota", tier: UserTierFree, used: quotaFreeInt, want: true},
		{name: "free: one byte over quota", tier: UserTierFree, used: quotaFreeInt + 1, want: true},
		// Pro tier (quota = 10 GB): usage that is one byte OVER the free
		// quota but still comfortably UNDER the pro quota proves the LARGER
		// pro quota is actually used.
		{name: "pro: over free quota but under pro quota", tier: UserTierPro, used: quotaFreeInt + 1, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Tier: tt.tier, StorageUsedBytes: tt.used}
			if got := u.IsOverQuota(); got != tt.want {
				t.Errorf("IsOverQuota() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrgBillingStorageQuota(t *testing.T) {
	tests := []struct {
		name      string
		seatLimit int
		want      int64
	}{
		{name: "zero seats", seatLimit: 0, want: 0},
		{name: "one seat", seatLimit: 1, want: quotaProInt},
		// Multi-seat proves quota is seats * 10 GB, not a fixed constant.
		{name: "three seats", seatLimit: 3, want: 3 * quotaProInt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &OrgBilling{SeatLimit: tt.seatLimit}
			if got := o.StorageQuota(); got != tt.want {
				t.Errorf("StorageQuota() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestOrgBillingIsOverQuota(t *testing.T) {
	tests := []struct {
		name      string
		seatLimit int
		used      int64
		want      bool
	}{
		// Zero seats => quota is 0, and 0 >= 0 is true: over immediately,
		// even with zero usage.
		{name: "zero seats, zero usage", seatLimit: 0, used: 0, want: true},
		{name: "zero seats, nonzero usage", seatLimit: 0, used: 1, want: true},
		// One seat (quota = 10 GB): boundary is >=.
		{name: "one seat: one byte under quota", seatLimit: 1, used: quotaProInt - 1, want: false},
		{name: "one seat: exactly at quota", seatLimit: 1, used: quotaProInt, want: true},
		{name: "one seat: one byte over quota", seatLimit: 1, used: quotaProInt + 1, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &OrgBilling{SeatLimit: tt.seatLimit, StorageUsedBytes: tt.used}
			if got := o.IsOverQuota(); got != tt.want {
				t.Errorf("IsOverQuota() = %v, want %v", got, tt.want)
			}
		})
	}
}
