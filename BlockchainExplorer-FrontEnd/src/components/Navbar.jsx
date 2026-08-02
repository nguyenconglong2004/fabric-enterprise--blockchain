import React from 'react';
import { NavLink, Link, useNavigate } from 'react-router-dom';
import { StatusPill } from './ui';
import { useAuth } from '../auth/AuthContext';

const links = [
  { to: '/transactions', label: 'Transactions' },
  { to: '/blocks', label: 'Blocks' },
  { to: '/transfer', label: 'Submit' },
  { to: '/profile', label: 'Wallet' },
];

const Navbar = ({ streamStatus = 'connecting', contractsCount = 0 }) => {
  const { isAuthenticated, account, logout } = useAuth();
  const navigate = useNavigate();

  const onSignOut = async () => {
    await logout();
    navigate('/login');
  };

  return (
    <header className="sticky top-0 z-40 border-b border-[var(--border)] bg-[rgba(7,11,18,0.82)] backdrop-blur-xl">
      <div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-4 py-3 sm:px-6">
        <Link to="/transactions" className="group flex items-center gap-3">
          <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-[var(--accent-dim)] ring-1 ring-[rgba(20,241,149,0.35)]">
            <span className="h-3 w-3 rounded-sm bg-[var(--accent)] shadow-[0_0_12px_rgba(20,241,149,0.7)]" />
          </span>
          <div>
            <p className="text-sm font-semibold tracking-tight text-[var(--text)] group-hover:text-[var(--accent)]">
              Fabric Explorer
            </p>
            <p className="text-[11px] text-[var(--muted)]">Enterprise ledger · live</p>
          </div>
        </Link>

        <nav className="hidden items-center gap-1 rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] p-1 md:flex">
          {links.map((link) => (
            <NavLink
              key={link.to}
              to={link.to}
              className={({ isActive }) =>
                `rounded-lg px-3.5 py-1.5 text-sm font-medium transition ${
                  isActive
                    ? 'bg-[var(--surface)] text-[var(--accent)] shadow-sm'
                    : 'text-[var(--muted)] hover:text-[var(--text)]'
                }`
              }
            >
              {link.label}
            </NavLink>
          ))}
        </nav>

        <div className="flex items-center gap-3">
          <div className="hidden text-right sm:block">
            <p className="text-[11px] uppercase tracking-wider text-[var(--muted)]">Contracts</p>
            <p className="text-sm font-semibold text-[var(--text)]">{contractsCount}</p>
          </div>
          {isAuthenticated ? (
            <div className="flex items-center gap-2">
              <Link
                to="/profile"
                className="hidden max-w-[9rem] truncate rounded-lg border border-[var(--border)] px-2.5 py-1 text-xs font-medium text-[var(--accent)] hover:border-[var(--accent)] sm:block"
                title={account?.address}
              >
                {account?.username}
              </Link>
              <button
                type="button"
                onClick={onSignOut}
                className="rounded-lg border border-[var(--border)] px-2.5 py-1 text-xs font-medium text-[var(--muted)] hover:border-[var(--danger)] hover:text-[var(--danger)]"
              >
                Sign out
              </button>
            </div>
          ) : (
            <Link
              to="/login"
              className="rounded-lg border border-[var(--border)] px-2.5 py-1 text-xs font-medium text-[var(--muted)] hover:border-[var(--accent)] hover:text-[var(--accent)]"
            >
              Sign in
            </Link>
          )}
          <StatusPill status={streamStatus} />
        </div>
      </div>

      <nav className="flex gap-1 overflow-x-auto border-t border-[var(--border-subtle)] px-4 py-2 md:hidden">
        {links.map((link) => (
          <NavLink
            key={link.to}
            to={link.to}
            className={({ isActive }) =>
              `whitespace-nowrap rounded-lg px-3 py-1.5 text-sm font-medium ${
                isActive ? 'bg-[var(--accent-dim)] text-[var(--accent)]' : 'text-[var(--muted)]'
              }`
            }
          >
            {link.label}
          </NavLink>
        ))}
        {isAuthenticated ? (
          <button
            type="button"
            onClick={onSignOut}
            className="whitespace-nowrap rounded-lg px-3 py-1.5 text-sm font-medium text-[var(--muted)]"
          >
            Sign out
          </button>
        ) : (
          <NavLink
            to="/login"
            className={({ isActive }) =>
              `whitespace-nowrap rounded-lg px-3 py-1.5 text-sm font-medium ${
                isActive ? 'bg-[var(--accent-dim)] text-[var(--accent)]' : 'text-[var(--muted)]'
              }`
            }
          >
            Sign in
          </NavLink>
        )}
      </nav>
    </header>
  );
};

export default Navbar;
