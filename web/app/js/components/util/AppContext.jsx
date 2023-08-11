import React from 'react';
import ApiHelpers from './ApiHelpers.jsx';

const Context = React.createContext({
  api: ApiHelpers(''),
});

export const withContext = Component => function componentWithContext(props) {
  return (
    <Context.Consumer>
      {ctx => <Component {...props} {...ctx} />}
    </Context.Consumer>
  );
};

export default Context;
