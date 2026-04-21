import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';

/*
* Instructions for adding resources to service mesh
*/
export const incompleteMeshMessage = name => {
  const unspecifiedResources = <Trans>unspecifiedResourcesMsg</Trans>;
  const inject = <code>linkerd inject k8s.yml | kubectl apply -f -</code>;

  return (
    <Typography variant="body2">
      <Trans>
        Add {name || unspecifiedResources } to the k8s.yml file<br /><br />
        Then run {inject} to add it to the service mesh
      </Trans>
    </Typography>
  );
};
