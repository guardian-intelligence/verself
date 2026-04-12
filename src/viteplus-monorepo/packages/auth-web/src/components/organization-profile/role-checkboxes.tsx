import { Checkbox } from "@forge-metal/ui/components/ui/checkbox";
import { Label } from "@forge-metal/ui/components/ui/label";

export interface RoleOption {
  readonly role_key: string;
  readonly display_name: string;
}

interface RoleCheckboxesProps {
  readonly roles: ReadonlyArray<RoleOption>;
  readonly value: ReadonlyArray<string>;
  readonly onChange: (next: Array<string>) => void;
  readonly disabled?: boolean;
  readonly error?: string | undefined;
  readonly legend?: string;
}

// Multi-select role picker shared between the invite form and the per-member
// row editor. Roles are code-owned constants now (`admin` / `member`); the
// owner role is a singleton transferred through a different flow and is not
// offered here.
export function RoleCheckboxes({
  roles,
  value,
  onChange,
  disabled,
  error,
  legend = "Roles",
}: RoleCheckboxesProps) {
  const selected = new Set(value);
  return (
    <fieldset className="space-y-3">
      <legend className="text-sm font-medium">{legend}</legend>
      <div className="grid gap-2">
        {roles.map((role) => {
          const checkboxId = `role-${legend.toLowerCase().replace(/\s+/g, "-")}-${role.role_key}`;
          const isChecked = selected.has(role.role_key);
          return (
            <Label
              key={role.role_key}
              htmlFor={checkboxId}
              className="flex items-start gap-3 rounded-md border border-border px-3 py-2 text-sm font-normal hover:bg-accent/50 has-disabled:opacity-60"
            >
              <Checkbox
                id={checkboxId}
                checked={isChecked}
                disabled={disabled}
                className="mt-0.5"
                onCheckedChange={(checked: boolean | "indeterminate") => {
                  const enabled = checked === true || checked === "indeterminate";
                  const next = enabled
                    ? Array.from(new Set([...value, role.role_key]))
                    : value.filter((roleKey) => roleKey !== role.role_key);
                  onChange(next);
                }}
              />
              <span className="min-w-0">
                <span className="block font-medium">{role.display_name}</span>
                <code className="break-all text-xs text-muted-foreground">{role.role_key}</code>
              </span>
            </Label>
          );
        })}
      </div>
      {error ? (
        <p className="text-sm text-destructive" role="alert">
          {error}
        </p>
      ) : null}
    </fieldset>
  );
}
