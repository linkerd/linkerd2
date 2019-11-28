import React from 'react';
import _isFunction from 'lodash/isFunction';
import _isUndefined from 'lodash/isUndefined';
import { usePageVisibility } from 'react-page-visibility';

export function handlePageVisibility(params) {
  const { prevVisibilityState, currentVisibilityState, onVisible, onHidden } = params;
  if (_isUndefined(prevVisibilityState) || _isUndefined(currentVisibilityState)) {
    return;
  }

  if (prevVisibilityState && !currentVisibilityState && _isFunction(onHidden)) {
    onHidden();
  }

  if (!prevVisibilityState && currentVisibilityState && _isFunction(onVisible)) {
    onVisible();
  }
}

export const withPageVisibility = WrappedComponent => {
  const Component = props => {
    const isVisible = usePageVisibility();
    return <WrappedComponent {...props} isPageVisible={isVisible} />;
  };

  return Component;
};
