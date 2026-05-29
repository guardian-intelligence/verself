Feature: Continue with Guardian for a Guardian Intelligence relying party

  Background:
    Given the OpenID Provider issuer is "https://verself.sh"
    And "guardianintelligence.org" is verified for the Guardian Intelligence organization
    And the QA human account belongs to the "guardian-intelligence" organization
    And the browser starts without Verself or Guardian relying-party cookies

  Scenario: Admin signs into Verself and creates a Guardian OIDC app
    Given I am on "https://verself.sh/login"
    Then I see "Sign in with Guardian"
    When I authenticate with a Guardian account
    Then I land in the Guardian Intelligence organization console

    When I open the OIDC application setup page
    And I create an application named "Guardian Intelligence canary <run-key>"
    And I set redirect URI to "https://guardianintelligence.org/_canary/guardian-oidc/callback"
    And I set post logout URI to "https://guardianintelligence.org/_canary/guardian-oidc"
    And I request scopes "openid profile email"
    Then IAM creates an active OIDC application
    And the application issuer is "https://verself.sh"
    And the application uses authorization code with PKCE
    And the app creation is recorded in governance audit evidence

  Scenario: User continues with Guardian from guardianintelligence.org
    Given the Guardian Intelligence canary page has the created client ID
    When I open "https://guardianintelligence.org/_canary/guardian-oidc"
    Then I see "Continue with Guardian"

    When I click "Continue with Guardian"
    Then the browser navigates to "https://verself.sh/oauth/v2/authorize"
    And the authorization request includes:
      | response_type         | code |
      | scope                 | openid profile email |
      | code_challenge_method | S256 |
      | redirect_uri          | https://guardianintelligence.org/_canary/guardian-oidc/callback |

    When Verself recognizes my existing Guardian browser session
    Then I can continue as the authenticated Guardian account
    And Verself redirects back to the Guardian Intelligence callback with code and state

    When the callback exchanges the code
    Then the ID token validates with issuer "https://verself.sh"
    And the audience matches the created client ID
    And the nonce matches the stored login transaction
    And the page shows "Signed in with Guardian"
    And no access token, refresh token, or ID token is visible in browser storage

  Scenario: Unverified redirect URI is rejected
    Given I am signed into the Guardian Intelligence organization console
    When I create an OIDC app with redirect URI "https://example.invalid/callback"
    Then IAM returns a validation problem
    And no Zitadel OIDC app is created
    And the denied operation is recorded with audit evidence
