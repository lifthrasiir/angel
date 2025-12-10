package gemini

import (
	"context"
	"fmt"
	"log"
	"time"
)

// LoginWithGoogle handles Google OAuth authentication flow including onboarding
func LoginWithGoogle(ctx context.Context, clientProvider HTTPClientProvider) (outProjectID string, err error) {
	// Create a temporary client to get ProjectID
	tempCaClient := NewCodeAssistClient(clientProvider, "", "geminicli")
	loadRes, loadErr := tempCaClient.LoadCodeAssist(ctx, LoadCodeAssistRequest{})
	if loadErr != nil {
		log.Printf("LoginWithGoogle: LoadCodeAssist failed: %v", loadErr)
		return "", loadErr
	}

	// If currentTier exists, user is already onboarded
	if loadRes.CurrentTier != nil {
		log.Printf("LoginWithGoogle: User already onboarded with tier %s", loadRes.CurrentTier.ID)

		if loadRes.CloudaicompanionProject == "" {
			return "", fmt.Errorf("no project ID available for onboarded user")
		}
		projectID := loadRes.CloudaicompanionProject

		// Set freeTierDataCollectionOptin to false for free tier users
		if loadRes.CurrentTier.ID == UserTierIDFree {
			if err := setFreeTierDataCollectionOptin(ctx, tempCaClient, projectID); err != nil {
				log.Printf("LoginWithGoogle: Failed to set freeTierDataCollectionOptin: %v", err)
				// Don't fail the entire process for this
			}
		}

		return projectID, nil
	}

	// User needs onboarding - proceed with onboarding flow
	return performOnboarding(ctx, tempCaClient, loadRes)
}

// performOnboarding handles the onboarding process for new users
func performOnboarding(ctx context.Context, tempCaClient *CodeAssistClient, loadRes *LoadCodeAssistResponse) (outProjectID string, err error) {
	// Determine user tier for onboarding
	userTierID := determineUserTier(loadRes)

	// Perform onboarding with LRO polling
	lroRes, err := performOnboardUserWithPolling(ctx, tempCaClient, OnboardUserRequest{TierID: &userTierID})
	if err != nil {
		log.Printf("LoginWithGoogle: OnboardUser failed: %v", err)
		return "", err
	}

	if lroRes.Response == nil || lroRes.Response.CloudaicompanionProject == nil || lroRes.Response.CloudaicompanionProject.ID == "" {
		return "", fmt.Errorf("onboardUser succeeded but returned empty or invalid project ID - user may be ineligible for service")
	}

	projectID := lroRes.Response.CloudaicompanionProject.ID
	log.Printf("LoginWithGoogle: Successfully onboarded with project ID: %s", projectID)

	// Set freeTierDataCollectionOptin to false for free tier users
	if userTierID == UserTierIDFree {
		if err := setFreeTierDataCollectionOptin(ctx, tempCaClient, projectID); err != nil {
			log.Printf("LoginWithGoogle: Failed to set freeTierDataCollectionOptin: %v", err)
			// Don't fail the entire process for this
		}
	}

	return projectID, nil
}

// determineUserTier determines the user tier from LoadCodeAssist response
func determineUserTier(loadRes *LoadCodeAssistResponse) UserTierID {
	for _, tier := range loadRes.AllowedTiers {
		if tier.IsDefault != nil && *tier.IsDefault {
			return tier.ID
		}
	}
	return UserTierIDLegacy
}

// performOnboardUserWithPolling handles the LRO polling for onboardUser
func performOnboardUserWithPolling(ctx context.Context, tempCaClient *CodeAssistClient, onboardReq OnboardUserRequest) (*LongRunningOperationResponse, error) {
	lroRes, err := tempCaClient.OnboardUser(ctx, onboardReq)
	if err != nil {
		return nil, fmt.Errorf("initial onboardUser call failed: %w", err)
	}

	// Poll until LRO is complete
	for lroRes.Done == nil || !*lroRes.Done {
		log.Printf("LoginWithGoogle: OnboardUser in progress, waiting 5 seconds...")

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			// Continue polling
		}

		lroRes, err = tempCaClient.OnboardUser(ctx, onboardReq)
		if err != nil {
			return nil, fmt.Errorf("polling onboardUser failed: %w", err)
		}
	}

	log.Printf("LoginWithGoogle: OnboardUser completed successfully")
	return lroRes, nil
}

// setFreeTierDataCollectionOptin sets freeTierDataCollectionOptin to false for free tier users
func setFreeTierDataCollectionOptin(ctx context.Context, tempCaClient *CodeAssistClient, projectID string) error {
	settingReq := SetCodeAssistGlobalUserSettingRequest{
		CloudaicompanionProject:     projectID,
		FreeTierDataCollectionOptin: false,
	}

	_, err := tempCaClient.SetCodeAssistGlobalUserSetting(ctx, settingReq)
	if err != nil {
		return fmt.Errorf("failed to set freeTierDataCollectionOptin to false: %w", err)
	}

	log.Printf("LoginWithGoogle: Successfully set freeTierDataCollectionOptin to false for project %s", projectID)
	return nil
}
