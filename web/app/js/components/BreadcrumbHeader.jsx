import PropTypes from 'prop-types';
import React from 'react';
import ReactRouterPropTypes from 'react-router-prop-types';
import { Trans } from '@lingui/macro';
import _chunk from 'lodash/chunk';
import _takeWhile from 'lodash/takeWhile';
import { friendlyTitle, isResource, singularResource } from './util/Utils.js';
import { withContext } from './util/AppContext.jsx';

const routeToCrumbTitle = {
  controlplane: <Trans>menuItemControlPlane</Trans>,
  tap: <Trans>menuItemTap</Trans>,
  top: <Trans>menuItemTop</Trans>,
  routes: <Trans>menuItemRoutes</Trans>,
  community: <Trans>menuItemCommunity</Trans>,
  gateways: <Trans>menuItemGateway</Trans>,
  extensions: <Trans>menuItemExtension</Trans>,
};

class BreadcrumbHeader extends React.Component {
  static processResourceDetailURL(segments) {
    if (segments.length === 4) {
      const splitSegments = _chunk(segments, 2);
      const resourceNameSegment = splitSegments[1];
      resourceNameSegment[0] = singularResource(resourceNameSegment[0]);
      return splitSegments[0].concat(resourceNameSegment.join('/'));
    } else {
      return segments;
    }
  }

  static convertURLToBreadcrumbs(location) {
    if (location.length === 0) {
      return [];
    } else {
      const segments = location.split('/').slice(1);
      const finalSegments = BreadcrumbHeader.processResourceDetailURL(segments);

      return finalSegments.map(segment => {
        const partialUrl = _takeWhile(segments, s => {
          return s !== segment;
        });

        if (partialUrl.length !== segments.length) {
          partialUrl.push(segment);
        }

        return {
          link: `/${partialUrl.join('/')}`,
          segment,
        };
      });
    }
  }

  static segmentToFriendlyTitle(segment, isResourceType) {
    if (isResourceType) {
      return routeToCrumbTitle[segment] || friendlyTitle(segment).plural;
    } else {
      return routeToCrumbTitle[segment] || segment;
    }
  }

  static renderBreadcrumbSegment(segment, numCrumbs, index) {
    const isMeshResource = isResource(segment);

    if (isMeshResource) {
      if (numCrumbs === 1 || index !== 0) {
        // If the segment is a K8s resource type, it should be pluralized if
        // the complete breadcrumb group describes a single-word list of
        // resources ("Namespaces") OR if the breadcrumb group describes a list
        // of resources within a specific namespace ("Namespace > linkerd >
        // Deployments")
        return BreadcrumbHeader.segmentToFriendlyTitle(segment, true);
      }
      return friendlyTitle(segment).singular;
    }
    return BreadcrumbHeader.segmentToFriendlyTitle(segment, false);
  }

  render() {
    const { pathPrefix, location } = this.props;
    const { pathname } = location;
    const breadcrumbs = BreadcrumbHeader.convertURLToBreadcrumbs(pathname.replace(pathPrefix, ''));

    return breadcrumbs.map((pathSegment, index) => {
      return (
        <span key={pathSegment.segment}>
          {BreadcrumbHeader.renderBreadcrumbSegment(pathSegment.segment, breadcrumbs.length, index)}
          { index < breadcrumbs.length - 1 ? ' > ' : null }
        </span>
      );
    });
  }
}

BreadcrumbHeader.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  location: ReactRouterPropTypes.location.isRequired,
  pathPrefix: PropTypes.string.isRequired,
};

export default withContext(BreadcrumbHeader);
