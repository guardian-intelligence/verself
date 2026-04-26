import type { IdentityApiClient } from "@verself/auth-web/components";
import {
  getMembers,
  getMemberCapabilities,
  getOrganization,
  listMyOrganizations,
  inviteMember,
  putMemberCapabilities,
  updateOrganization,
  updateMemberRoles,
} from "~/server-fns/api";

// Adapter that wires console's bearer-forwarding server fns into the
// shape @verself/auth-web's organization components consume. The browser
// never sees a Zitadel token: each call goes through createServerFn, which
// reads the session cookie server-side and forwards the bearer onward.
export const identityApiClient: IdentityApiClient = {
  getOrganization: () => getOrganization(),
  listMyOrganizations: () => listMyOrganizations(),
  updateOrganization: (data) => updateOrganization({ data }),
  listMembers: () => getMembers(),
  getMemberCapabilities: () => getMemberCapabilities(),
  putMemberCapabilities: (data) => putMemberCapabilities({ data }),
  inviteMember: (data) => inviteMember({ data }),
  updateMemberRoles: (data) => updateMemberRoles({ data }),
};
