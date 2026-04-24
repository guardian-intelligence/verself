import { createContext, type ReactNode, useContext, useMemo } from "react";

type ShellConfig = {
  readonly platformOrigin: string;
};

const ShellConfigContext = createContext<ShellConfig | null>(null);

export function ShellConfigProvider({
  children,
  platformOrigin,
}: {
  readonly children: ReactNode;
  readonly platformOrigin: string;
}) {
  const value = useMemo(() => ({ platformOrigin }), [platformOrigin]);
  return <ShellConfigContext.Provider value={value}>{children}</ShellConfigContext.Provider>;
}

export function useShellConfig(): ShellConfig {
  const value = useContext(ShellConfigContext);
  if (!value) {
    throw new Error("ShellConfigProvider is required");
  }
  return value;
}
