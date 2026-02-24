# Skill: Add New Provider

> **Invocation**: When a user asks to add, create, or implement a new AI provider.

Read and follow the full skill specification in `docs/skills/add-new-provider.md`.

The user's request is to add a new provider. The provider name may be part of their message.

Follow all phases in order:
1. Phase 0: Quiz the user for required information
2. Phase 1: Research the provider's API
3. Phase 2: Create the provider package
4. Phase 3: Configure the dashboard widget
5. Phase 4: Register the provider and add auto-detection
6. Phase 5: Write tests
7. Phase 6: Verify with build + test + vet

Do NOT skip the quiz phase. Do NOT proceed without all answers.
