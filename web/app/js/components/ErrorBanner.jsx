import { apiErrorPropType } from './util/ApiHelpers.jsx';
import ErrorSnackbar from './util/ErrorSnackbar.jsx';
import React from 'react';

const ErrorMessage = props => {
  const { statusText, error, url, status } = props.message;
  let message = (
    <div>
      { !status && !statusText ? null : <div>{status} {statusText}</div> }
      { !error ? null : <div>{error}</div> }
      { !url ? null : <div>{url}</div> }
    </div>
  );

  return <ErrorSnackbar message={message} />;
};

ErrorMessage.propTypes = {
  message: apiErrorPropType
};

ErrorMessage.defaultProps = {
  message: {
    status: null,
    statusText: "An error occured",
    url: "",
    error: ""
  },
};

export default ErrorMessage;
