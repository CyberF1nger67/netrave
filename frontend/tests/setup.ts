import '@testing-library/jest-dom/vitest';
import { configure } from '@testing-library/react';

// Routes and pages are loaded lazily; on a cold run a chunk can take longer than
// the default 1000 ms of waitFor/findBy*. Keep this below vitest's testTimeout (see vitest.config.ts).
configure({ asyncUtilTimeout: 20_000 });
