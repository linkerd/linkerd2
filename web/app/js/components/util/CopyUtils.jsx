import React from 'react';
import { Trans } from 'react-i18next';
import Typography from '@material-ui/core/Typography';

/*
* Instructions for adding resources to service mesh
*/
export const incompleteMeshMessage = name => {
  return (
    <Typography>
      <Trans ns="serviceMesh" i18nKey="incompleteMeshMessage" count={name ? 1 : 2}>
        Add {{name}} to the k8s.yml file<br /><br />Then run <code>linkerd inject k8s.yml | kubectl apply -f -</code> to add it to the service mesh
      </Trans>
    </Typography>
  );
};
