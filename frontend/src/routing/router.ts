import { createBrowserRouter } from 'react-router-dom';
import { routeTree } from './routes';

// Regular URLs without "#". Direct opening and refreshing of nested pages
// is handled by the server: for routes without a file it serves index.html.
export const router = createBrowserRouter(routeTree);
