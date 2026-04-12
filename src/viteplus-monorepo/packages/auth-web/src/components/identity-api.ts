import { createContext, createElement, type ReactNode, useContext } from "react";
import type {
  InviteMemberRequest,
  InviteMemberResponse,
  Member,
  MemberCapabilities,
  Organization,
  PutMemberCapabilitiesRequest,
  UpdateMemberRolesRequest,
} from "./types.ts";

// IdentityApiClient is the contract every consumer fills with its own
// server-fn-backed adapter. The package never imports server functions or
// holds bearer tokens — keeping this surface as the boundary lets each app
// own its bearer-forwarding layer (per the auth model: browser code never
// holds Zitadel tokens).
export interface IdentityApiClient {
  getOrganization: () => Promise<Organization>;
  listMembers: () => Promise<ReadonlyArray<Member>>;
  getMemberCapabilities: () => Promise<MemberCapabilities>;
  putMemberCapabilities: (input: PutMemberCapabilitiesRequest) => Promise<MemberCapabilities>;
  inviteMember: (input: InviteMemberRequest) => Promise<InviteMemberResponse>;
  updateMemberRoles: (input: UpdateMemberRolesRequest) => Promise<Member>;
}

const IdentityApiContext = createContext<IdentityApiClient | null>(null);

export interface IdentityApiProviderProps {
  client: IdentityApiClient;
  children?: ReactNode;
}

export function IdentityApiProvider({ client, children }: IdentityApiProviderProps) {
  return createElement(IdentityApiContext.Provider, { value: client }, children);
}

export function useIdentityApi(): IdentityApiClient {
  const value = useContext(IdentityApiContext);
  if (!value) {
    throw new Error(
      "useIdentityApi() requires an <IdentityApiProvider> ancestor — wrap the authenticated subtree.",
    );
  }
  return value;
}
