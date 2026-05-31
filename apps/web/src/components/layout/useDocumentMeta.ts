import { useEffect } from 'react';

const LOGO_FAVICON_HREF = '/favicon.svg';

export function useDocumentMeta(title: string) {
  useEffect(() => {
    document.title = `${title} · Relay API`;
  }, [title]);

  useEffect(() => {
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');

    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }

    link.type = 'image/svg+xml';
    link.href = LOGO_FAVICON_HREF;
  }, []);
}
