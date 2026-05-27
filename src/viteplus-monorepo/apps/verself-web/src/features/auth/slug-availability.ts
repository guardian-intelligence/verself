import { checkOrganizationSlug } from "~/server-fns/auth";
import { organizationSlugPattern, slugify } from "./form-schemas";

export async function organizationSlugAvailabilityError(
  slugValue: string,
): Promise<string | undefined> {
  const slug = slugify(slugValue);
  if (!slug || !organizationSlugPattern.test(slug)) return undefined;
  try {
    const result = await checkOrganizationSlug({ data: { slug } });
    return result.available ? undefined : "That organization URL is already taken.";
  } catch {
    return "Could not check that organization URL.";
  }
}
