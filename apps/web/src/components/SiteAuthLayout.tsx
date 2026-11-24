import { useId, type ReactNode } from "react";
import "./site-auth.css";

interface SiteAuthLayoutProps {
  title: string;
  description: string;
  children: ReactNode;
}

export function SiteAuthLayout({ title, description, children }: SiteAuthLayoutProps) {
  const titleId = useId();

  return (
    <main className="site-auth-page" aria-labelledby={titleId}>
      <div className="site-auth-shell">
        <header className="site-auth-heading">
          <a className="site-auth-logo" href="/" aria-label="BUGO 首页">
            <img src="/assets/favicon.png?v=bugo" width={850} height={216} alt="BUGO" />
          </a>
          <h1 id={titleId}>{title}</h1>
          <p>{description}</p>
        </header>
        {children}
      </div>
    </main>
  );
}
