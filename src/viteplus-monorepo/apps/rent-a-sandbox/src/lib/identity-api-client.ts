import type { IdentityApiClient } from "@forge-metal/auth-web/components";
import {
  getMembers,
  getMemberCapabilities,
  getOrganization,
  inviteMember,
  putMemberCapabilities,
  updateMemberRoles,
} from "~/server-fns/api";

// Adapter that wires rent-a-sandbox's bearer-forwarding server fns into the
// shape @forge-metal/auth-web's organization components consume. The browser
// never sees a Zitadel token: each call goes through createServerFn, which
// reads the session cookie server-side and forwards the bearer onward.
export const identityApiClient: IdentityApiClient = {
  getOrganization: () => getOrganization(),
  listMembers: () => getMembers(),
  getMemberCapabilities: () => getMemberCapabilities(),
  putMemberCapabilities: (data) => putMemberCapabilities({ data }),
  inviteMember: (data) => inviteMember({ data }),
  updateMemberRoles: (data) => updateMemberRoles({ data }),
};
