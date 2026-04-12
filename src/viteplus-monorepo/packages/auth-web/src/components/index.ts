// Clerk-shaped React surface for organizations, members, and member capabilities.
// Pair this with @forge-metal/auth-web/react's <AuthProvider>: wrap your
// authenticated subtree in <IdentityApiProvider client={...}> and these
// components do the rest.

export { IdentityApiProvider, useIdentityApi, type IdentityApiClient } from "./identity-api.ts";
export type {
  InviteMemberRequest,
  InviteMemberResponse,
  Member,
  MemberCapabilities,
  MemberCapabilitiesDocument,
  MemberCapability,
  Operation,
  Operations,
  Organization,
  PutMemberCapabilitiesRequest,
  ServiceOperations,
  UpdateMemberRolesRequest,
} from "./types.ts";

export {
  invalidateOrganizationQueries,
  loadOrganizationPage,
  organizationMembersQuery,
  organizationMemberCapabilitiesQuery,
  organizationOperationsQuery,
  organizationQuery,
} from "./queries.ts";
export {
  useInviteMemberMutation,
  usePutMemberCapabilitiesMutation,
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
