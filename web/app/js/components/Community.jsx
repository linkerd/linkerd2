import React, { useEffect, useState } from 'react';
import _has from 'lodash/has';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  iframe: {
    border: '0px',
    width: '100%',
    overflow: 'hidden',
  },
});

const Community = ({ classes }) => {
  const [iframeHeight, setIframeHeight] = useState(0);
  useEffect(() => {
    // We add 5px to avoid cutting box shadow
    const setFromIframeEvent = e => {
      if (!_has(e.data, 'dashboardHeight')) {
        return;
      }
      setIframeHeight(e.data.dashboardHeight + 5);
    };
    window.addEventListener('message', setFromIframeEvent);
    return () => {
      window.removeEventListener('message', setFromIframeEvent);
    };
  }, []);

  return (
    <iframe
      title="Community"
      src="https://linkerd.io/dashboard/"
      scrolling="no"
      style={{ height: iframeHeight > 0 ? iframeHeight : '100vh' }}
      className={classes.iframe} />
  );
};

export default withStyles(styles)(Community);
