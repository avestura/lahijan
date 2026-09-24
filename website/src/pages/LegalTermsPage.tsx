/**
 * Terms of service (`/legal/terms`).
 *
 * Boilerplate per WS-19: each deployer is responsible for customizing the
 * copy.
 */
import { LegalDocument } from "@/components/marketing/LegalDocument";
import { SeoHead } from "@/components/seo/SeoHead";

const TERMS_SECTIONS = ["scope", "accounts", "acceptable", "billing", "contact"] as const;

export function LegalTermsPage() {
  return (
    <>
      <SeoHead
        titleKey="page.legal.terms.title"
        descriptionKey="page.legal.terms.lead"
        path="/legal/terms"
      />
      <LegalDocument prefix="page.legal.terms" sections={TERMS_SECTIONS} />
    </>
  );
}
