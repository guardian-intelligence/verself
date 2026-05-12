// Clerk-shaped React surface for organizations and members.
// Pair this with @verself/auth-web/react's <AuthProvider>: wrap your
// authenticated subtree in <IAMApiProvider client={...}> and these
// components do the rest.

export { IAMApiProvider, useIAMApi, type IAMApiClient } from "./iam-api.ts";
export type {
  Member,
  Organization,
  OrganizationMetadata,
  UpdateOrganizationRequest,
  UpdateMemberRolesRequest,
} from "./types.ts";

export {
  availableOrganizationMetadataQuery,
  invalidateOrganizationQueries,
  loadOrganizationPage,
  organizationMembersQuery,
  organizationQuery,
  type OrganizationMetadataValue,
} from "./queries.ts";
export {
  useUpdateOrganizationMutation,
  useUpdateMemberRolesMutation,
} from "./mutations.ts";

export {
  OrganizationProfile,
  type OrganizationProfileProps,
} from "./organization-profile/index.tsx";

export {
  Protect,
  SignedIn,
  SignedOut,
  SignInButton,
  UserButton,
  usePermissions,
  type ProtectProps,
  type SignInButtonProps,
  type UserButtonProps,
} from "./clerk-shape.tsx";
