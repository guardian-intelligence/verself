import type { IAMApiClient } from "@verself/auth-web/components";
import {
  createOrganization,
  getMembers,
  getOrganization,
  inviteMember,
  listMyOrganizations,
  updateOrganization,
} from "~/server-fns/api";

// Adapter that wires console's bearer-forwarding server fns into the
// shape @verself/auth-web's organization components consume. The browser
// never sees a Zitadel token: each call goes through createServerFn, which
// reads the session cookie server-side and forwards the bearer onward.
export const iamApiClient: IAMApiClient = {
  createOrganization: (data) => createOrganization({ data }),
  getOrganization: () => getOrganization(),
  listMyOrganizations: () => listMyOrganizations(),
  updateOrganization: (data) => updateOrganization({ data }),
  listMembers: () => getMembers(),
  inviteMember: (data) =>
    inviteMember({
      data: {
        email: data.email,
        ...(data.given_name !== undefined ? { given_name: data.given_name } : {}),
        ...(data.family_name !== undefined ? { family_name: data.family_name } : {}),
        ...(data.roles ? { roles: [...data.roles] } : {}),
      },
    }),
};
