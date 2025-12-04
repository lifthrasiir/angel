package gemini

import (
	"context"
	"fmt"
	"log"
	"time"
)

// LoginWithGoogle handles Google OAuth authentication flow including onboarding
func LoginWithGoogle(ctx context.Context, clientProvider HTTPClientProvider, projectID string) (outProjectID string, err error) {
	// Create a temporary client to get ProjectID
	tempCaClient := NewCodeAssistClient(clientProvider, "")
	loadReq := LoadCodeAssistRequest{
		CloudaicompanionProject: projectID, // Will be empty initially
		Metadata: &ClientMetadata{
			IdeType:     "IDE_UNSPECIFIED",
			Platform:    "PLATFORM_UNSPECIFIED",
			PluginType:  "GEMINI",
			DuetProject: projectID,
		},
	}

	loadRes, loadErr := tempCaClient.LoadCodeAssist(ctx, loadReq)
	if loadErr != nil {
		log.Printf("LoginWithGoogle: LoadCodeAssist failed: %v", loadErr)
		return "", loadErr
	}

	// If currentTier exists, user is already onboarded
	if loadRes.CurrentTier != nil {
		log.Printf("LoginWithGoogle: User already onboarded with tier %s", loadRes.CurrentTier.ID)

		if loadRes.CloudaicompanionProject != "" {
			projectID = loadRes.CloudaicompanionProject
		} else if projectID != "" {
			// Use existing project ID
		} else {
			return "", fmt.Errorf("no project ID available for onboarded user")
		}

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
	return performOnboarding(ctx, tempCaClient, projectID, loadRes)
}

// performOnboarding handles the onboarding process for new users
func performOnboarding(ctx context.Context, tempCaClient *CodeAssistClient, projectID string, loadRes *LoadCodeAssistResponse) (outProjectID string, err error) {
	// Determine user tier for onboarding
	userTierID := determineUserTier(loadRes)

	onboardReq := OnboardUserRequest{
		TierID: &userTierID,
		Metadata: &ClientMetadata{
			IdeType:    "IDE_UNSPECIFIED",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	}
	if userTierID != UserTierIDFree {
		// The free tier uses a managed google cloud project.
		// Setting a project in the `onboardUser` request causes a `Precondition Failed` error.
		onboardReq.CloudaicompanionProject = projectID
		onboardReq.Metadata.DuetProject = projectID
	}

	// Perform onboarding with LRO polling
	lroRes, err := performOnboardUserWithPolling(ctx, tempCaClient, onboardReq)
	if err != nil {
		log.Printf("LoginWithGoogle: OnboardUser failed: %v", err)
		return "", err
	}

	if lroRes.Response != nil && lroRes.Response.CloudaicompanionProject != nil && lroRes.Response.CloudaicompanionProject.ID != "" {
		projectID = lroRes.Response.CloudaicompanionProject.ID
		log.Printf("LoginWithGoogle: Successfully onboarded with project ID: %s", projectID)

		// Set freeTierDataCollectionOptin to false for free tier users
		if userTierID == UserTierIDFree {
			if err := setFreeTierDataCollectionOptin(ctx, tempCaClient, projectID); err != nil {
				log.Printf("LoginWithGoogle: Failed to set freeTierDataCollectionOptin: %v", err)
				// Don't fail the entire process for this
			}
		}

		return projectID, nil
	} else {
		return "", fmt.Errorf("onboardUser succeeded but returned empty or invalid project ID - user may be ineligible for service")
	}
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
