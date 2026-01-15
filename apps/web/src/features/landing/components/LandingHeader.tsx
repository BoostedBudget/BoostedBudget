/**
 * LandingHeader Component
 *
 * Sticky navigation header for the landing page with logo, nav links, and CTA.
 * Issue #699: Landing Page Design Improvements
 */

import { useState, useEffect } from 'react'
import { useClerk } from '@clerk/react-router'
import { Link } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Menu, X, Rocket } from 'lucide-react'

const navLinks = [
  { label: 'Features', href: '#features' },
  { label: 'How It Works', href: '#how-it-works' },
  { label: 'Pricing', href: '/pricing' },
  { label: 'FAQ', href: '#faq' },
]

function NavLink({
  link,
  isScrolled,
  onNavigate,
}: {
  link: (typeof navLinks)[0]
  isScrolled: boolean
  onNavigate?: () => void
}) {
  const baseClass = `text-sm font-medium transition-colors hover:text-blue-500 ${
    isScrolled ? 'text-slate-600 dark:text-slate-300' : 'text-white/80 hover:text-white'
  }`

  if (link.href.startsWith('#')) {
    return (
      <button
        onClick={() => {
          const element = document.querySelector(link.href)
          element?.scrollIntoView({ behavior: 'smooth' })
          onNavigate?.()
        }}
        className={baseClass}
      >
        {link.label}
      </button>
    )
  }

  return (
    <Link to={link.href} className={baseClass} onClick={onNavigate}>
      {link.label}
    </Link>
  )
}

export function LandingHeader() {
  const { openSignUp, openSignIn } = useClerk()
  const [isScrolled, setIsScrolled] = useState(false)
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false)

  useEffect(() => {
    const handleScroll = () => {
      setIsScrolled(window.scrollY > 20)
    }
    window.addEventListener('scroll', handleScroll)
    return () => window.removeEventListener('scroll', handleScroll)
  }, [])

  return (
    <header
      role="banner"
      className={`fixed left-0 right-0 top-0 z-50 transition-all duration-300 ${
        isScrolled
          ? 'bg-white/95 shadow-sm backdrop-blur-sm dark:bg-slate-900/95'
          : 'bg-transparent'
      }`}
    >
      <nav className="mx-auto flex max-w-7xl items-center justify-between px-4 py-4 sm:px-6 lg:px-8">
        {/* Logo */}
        <Link
          to="/"
          className="flex items-center gap-2"
          onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })}
        >
          <div className={`flex h-9 w-9 items-center justify-center rounded-lg ${
            isScrolled ? 'bg-blue-600' : 'bg-white/10'
          }`}>
            <Rocket className="h-5 w-5 text-white" />
          </div>
          <span className={`text-xl font-bold ${
            isScrolled ? 'text-slate-900 dark:text-white' : 'text-white'
          }`}>
            BoostedBudget
          </span>
        </Link>

        {/* Desktop Navigation */}
        <div className="hidden items-center gap-8 md:flex">
          {navLinks.map((link) => (
            <NavLink key={link.label} link={link} isScrolled={isScrolled} />
          ))}
        </div>

        {/* Desktop CTA */}
        <div className="hidden items-center gap-3 md:flex">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => openSignIn()}
            className={isScrolled
              ? 'text-slate-600 hover:text-slate-900 dark:text-slate-300'
              : 'text-white/80 hover:bg-white/10 hover:text-white'
            }
          >
            Log In
          </Button>
          <Button
            size="sm"
            onClick={() => openSignUp()}
            className="bg-blue-600 hover:bg-blue-500"
          >
            Start Free
          </Button>
        </div>

        {/* Mobile Menu Button */}
        <button
          className="md:hidden"
          onClick={() => setIsMobileMenuOpen(!isMobileMenuOpen)}
          aria-label={isMobileMenuOpen ? 'Close menu' : 'Open menu'}
        >
          {isMobileMenuOpen ? (
            <X className={`h-6 w-6 ${isScrolled ? 'text-slate-900 dark:text-white' : 'text-white'}`} />
          ) : (
            <Menu className={`h-6 w-6 ${isScrolled ? 'text-slate-900 dark:text-white' : 'text-white'}`} />
          )}
        </button>
      </nav>

      {/* Mobile Menu */}
      {isMobileMenuOpen && (
        <div className="border-t border-slate-200 bg-white px-4 py-4 dark:border-slate-700 dark:bg-slate-900 md:hidden">
          <div className="flex flex-col gap-4">
            {navLinks.map((link) => (
              <NavLink
                key={link.label}
                link={link}
                isScrolled={true}
                onNavigate={() => setIsMobileMenuOpen(false)}
              />
            ))}
            <div className="flex flex-col gap-2 border-t border-slate-200 pt-4 dark:border-slate-700">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setIsMobileMenuOpen(false)
                  openSignIn()
                }}
                className="w-full"
              >
                Log In
              </Button>
              <Button
                size="sm"
                onClick={() => {
                  setIsMobileMenuOpen(false)
                  openSignUp()
                }}
                className="w-full bg-blue-600 hover:bg-blue-500"
              >
                Start Free
              </Button>
            </div>
          </div>
        </div>
      )}
    </header>
  )
}
