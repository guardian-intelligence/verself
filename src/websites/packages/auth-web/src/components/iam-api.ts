import { createContext, createElement, type ReactNode, useContext } from "react";
import type {
  Member,
  Organization,
  OrganizationMetadata,
  UpdateOrganizationRequest,
  UpdateMemberRolesRequest,
} from "./types.ts";

// IAMApiClient is the contract every consumer fills with its own
// server-fn-backed adapter. The package never imports server functions or
// holds bearer tokens — keeping this surface as the boundary lets each app
// own its bearer-forwarding layer (per the auth model: browser code never
// holds Zitadel tokens).
export interface IAMApiClient {
  getOrganization: () => Promise<Organization>;
  listMyOrganizations: () => Promise<ReadonlyArray<OrganizationMetadata>>;
  updateOrganization: (input: UpdateOrganizationRequest) => Promise<Organization>;
  listMembers: () => Promise<ReadonlyArray<Member>>;
  updateMemberRoles: (input: UpdateMemberRolesRequest) => Promise<Member>;
}

const IAMApiContext = createContext<IAMApiClient | null>(null);

export interface IAMApiProviderProps {
  client: IAMApiClient;
  children?: ReactNode;
}

export function IAMApiProvider({ client, children }: IAMApiProviderProps) {
  return createElement(IAMApiContext.Provider, { value: client }, children);
}

export function useIAMApi(): IAMApiClient {
  const value = useContext(IAMApiContext);
  if (!value) {
    throw new Error(
      "useIAMApi() requires an <IAMApiProvider> ancestor — wrap the authenticated subtree.",
    );
  }
  return value;
}
