/**
 * Privacy policy (`/legal/privacy`).
 *
 * Boilerplate per WS-19: each deployer is responsible for customizing the
 * copy. The structured sections below are placeholders a deployer can edit
 * in their locale bundle (or replace wholesale).
 */
import { LegalDocument } from "@/components/marketing/LegalDocument";
import { SeoHead } from "@/components/seo/SeoHead";

const PRIVACY_SECTIONS = ["scope", "data", "retention", "contact"] as const;

export function LegalPrivacyPage() {
  return (
    <>
      <SeoHead
        titleKey="page.legal.privacy.title"
        descriptionKey="page.legal.privacy.lead"
        path="/legal/privacy"
      />
      <LegalDocument prefix="page.legal.privacy" sections={PRIVACY_SECTIONS} />
    </>
  );
}
