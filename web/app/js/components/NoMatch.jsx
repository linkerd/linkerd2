import React from 'react';
import { Trans } from '@lingui/macro';

const NoMatch = function() {
  return (
    <div>
      <h3>404</h3>
      <div>
        <Trans>
          404Msg
        </Trans>
      </div>
    </div>
  );
};

export default NoMatch;
