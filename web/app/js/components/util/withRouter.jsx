import React from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';

export function withRouter(Component) {
    function ComponentWithRouterProp(props) {
        const location = useLocation();
        const navigate = useNavigate();
        const params = useParams();

        // Map v6 hooks to v5 props: history (with push/replace), location, match
        return (
            <Component
                {...props}
                location={location}
                history={{
                    push: navigate,
                    replace: (path, state) => navigate(path, { replace: true, state }),
                    location,
                }}
                match={{ params, url: location.pathname }}
            />
        );
    }

    return ComponentWithRouterProp;
}
