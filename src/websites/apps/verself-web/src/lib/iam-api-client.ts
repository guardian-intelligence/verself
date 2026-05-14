import type { IAMApiClient } from "@verself/auth-web/components";
import {
  getMembers,
  getOrganization,
  listMyOrganizations,
  updateOrganization,
} from "~/server-fns/api";

// Adapter that wires console's bearer-forwarding server fns into the
// shape @verself/auth-web's organization components consume. The browser
// never sees a Zitadel token: each call goes through createServerFn, which
// reads the session cookie server-side and forwards the bearer onward.
export const iamApiClient: IAMApiClient = {
  getOrganization: () => getOrganization(),
  listMyOrganizations: () => listMyOrganizations(),
  updateOrganization: (data) => updateOrganization({ data }),
  listMembers: () => getMembers(),
};
