/**
 * Globe — a thin project-local wrapper around lucide's Globe icon.
 *
 * lucide-react 0.456 still ships `Globe`, but the Sidebar already wraps it
 * behind a local component so the icon source can be swapped project-wide
 * without churning every call site. The dashboard reuses the same idea for
 * consistency.
 */
export function Globe({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="10" />
      <path d="M2 12h20" />
      <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
    </svg>
  );
}
