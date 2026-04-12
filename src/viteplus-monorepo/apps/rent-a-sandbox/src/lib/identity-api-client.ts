import type { IdentityApiClient } from "@forge-metal/auth-web/components";
import {
  getMembers,
  getOperations,
  getOrganization,
  getPolicy,
  inviteMember,
  putPolicy,
  updateMemberRoles,
} from "~/server-fns/api";

// Adapter that wires rent-a-sandbox's bearer-forwarding server fns into the
// shape @forge-metal/auth-web's organization components consume. The browser
// never sees a Zitadel token: each call goes through createServerFn, which
// reads the session cookie server-side and forwards the bearer onward.
export const identityApiClient: IdentityApiClient = {
  getOrganization: () => getOrganization(),
  listMembers: () => getMembers(),
  listOperations: () => getOperations(),
  getPolicy: () => getPolicy(),
  putPolicy: (data) => putPolicy({ data }),
  inviteMember: (data) => inviteMember({ data }),
  updateMemberRoles: (data) => updateMemberRoles({ data }),
};
